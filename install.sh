#!/bin/sh
# anonseed installer: download the latest release, verify its sha256 checksum, and
# install the anonseed binary.
#
#   curl -fsSL https://github.com/wighawag/anonseed/releases/latest/download/install.sh | sh
#
# anonseed seeds a tool's config into an anonymized identity by writing anonctl's
# host state under /etc/anonctl (a root-owned path) and self-elevates to do so, so
# this installs to /usr/local/bin by DEFAULT (root-writable, on a shared system
# PATH), NOT to ~/.local/bin: a per-user binary that then self-elevates would be a
# surprising path for a root operation.
#
# Options (environment variables):
#   ANONSEED_VERSION  version tag to install (default: latest, e.g. v0.1.0)
#   PREFIX            install dir (default: /usr/local/bin)
#
# anonseed targets the Linux host anonctl runs on (it writes /etc/anonctl state and
# self-elevates via sudo), so the released binaries are Linux-only. This script
# refuses to install on a non-Linux uname.
set -eu

REPO="wighawag/anonseed"
BIN="anonseed"

info() { printf '%s\n' "anonseed-install: $*" >&2; }
err() {
	printf '%s\n' "anonseed-install: error: $*" >&2
	exit 1
}

# --- platform ---------------------------------------------------------------
os="$(uname -s)"
[ "$os" = "Linux" ] || err "anonseed is Linux-only (got $os). It seeds anonctl host state under /etc/anonctl and self-elevates, which only applies on the Linux host anonctl runs on."

arch="$(uname -m)"
case "$arch" in
x86_64 | amd64) target="linux_amd64" ;;
aarch64 | arm64) target="linux_arm64" ;;
armv7l | armv7) target="linux_armv7" ;;
armv6l | armv6) target="linux_armv6" ;;
arm*)
	# Unqualified arm: prefer armv7, the common 32-bit Raspberry Pi target.
	info "unrecognised arm variant '$arch'; defaulting to armv7 (set the tarball manually if wrong)"
	target="linux_armv7"
	;;
*) err "unsupported architecture '$arch' (supported: amd64, arm64, armv7, armv6)" ;;
esac

# --- tools ------------------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
	dl() { curl -fsSL "$1" -o "$2"; }
	dlout() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
	dl() { wget -qO "$2" "$1"; }
	dlout() { wget -qO- "$1"; }
else
	err "need curl or wget on PATH"
fi

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
	err "need sha256sum or shasum to verify the download"
fi

# --- version ----------------------------------------------------------------
version="${ANONSEED_VERSION:-}"
if [ -z "$version" ]; then
	info "resolving the latest release..."
	version="$(dlout "https://api.github.com/repos/$REPO/releases/latest" |
		grep '"tag_name"' | head -n1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
	[ -n "$version" ] || err "could not resolve the latest release tag (set ANONSEED_VERSION=vX.Y.Z)"
fi
# The archive uses the version WITHOUT the leading v (e.g. v0.1.0 -> 0.1.0).
ver_noV="${version#v}"

archive="${BIN}_${ver_noV}_${target}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

info "installing $BIN $version ($target)"

# --- download + verify ------------------------------------------------------
tmp="$(mktemp -d "${TMPDIR:-/tmp}/anonseed-install.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM

info "downloading $archive"
dl "$base/$archive" "$tmp/$archive" || err "download failed: $base/$archive"
dl "$base/checksums.txt" "$tmp/checksums.txt" || err "download failed: $base/checksums.txt"

# NEVER install an unverified anonymity tool: the checksum must be present AND
# match, or we abort before touching the filesystem.
want="$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')"
[ -n "$want" ] || err "no checksum for $archive in checksums.txt"
got="$(sha256 "$tmp/$archive")"
[ "$want" = "$got" ] || err "checksum mismatch for $archive
  expected: $want
  got:      $got"
info "checksum ok"

tar -xzf "$tmp/$archive" -C "$tmp" "$BIN" || err "failed to extract $BIN"

# --- install dir ------------------------------------------------------------
# Default to /usr/local/bin (root-writable, on a shared system PATH). anonseed
# writes root-owned /etc/anonctl state and self-elevates, so a per-user
# ~/.local/bin is a surprising home for it.
dest="${PREFIX:-/usr/local/bin}"
mkdir -p "$dest" || err "cannot create install dir $dest (try: sudo sh, or PREFIX=/usr/local/bin sudo sh)"

if mv "$tmp/$BIN" "$dest/$BIN" 2>/dev/null; then :; else
	cp "$tmp/$BIN" "$dest/$BIN" || err "cannot write $dest/$BIN (re-run with sudo)"
fi
chmod +x "$dest/$BIN"

info "installed:"
info "  $dest/$BIN"

# --- PATH hint --------------------------------------------------------------
case ":$PATH:" in
*":$dest:"*) ;;
*)
	info ""
	info "NOTE: $dest is not on your PATH. Add it, e.g.:"
	info "  echo 'export PATH=\"$dest:\$PATH\"' >> ~/.profile && . ~/.profile"
	;;
esac

info ""
info "done. Seed a tool's config into an anonymized identity, e.g.:"
info "  sudo $BIN pi --endpoint 127.0.0.1:11434"
