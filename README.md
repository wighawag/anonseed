# anonseed

`anonseed` is a Go CLI that seeds the configuration a local-service-using tool needs into an anonymized identity, and declares the one direct-egress IP exception that tool needs (e.g. a LAN/loopback model server), so the tool is ready to run anonymized. It is a config-seeding tool, NOT an account provisioner and NOT a runtime launcher.

It ships built-in seed types as subcommands (the first is `pi`) and targets anonctl's box-wide defaults today (`/etc/anonctl/default-home/` + `/etc/anonctl/defaults.json`), with anonbox as a future target. Part of the anonctl / netcage / anonbox / anoncore family.

## Install

Linux only (anonseed writes anonctl's host state under `/etc/anonctl` and self-elevates for the root-owned write):

```sh
curl -fsSL https://github.com/wighawag/anonseed/releases/latest/download/install.sh | sh
```

The installer downloads the release for your architecture, verifies its sha256 checksum, and installs the `anonseed` binary to `/usr/local/bin` (override with `PREFIX`, pin a version with `ANONSEED_VERSION=vX.Y.Z`).

Or with Go:

```sh
go install github.com/wighawag/anonseed@latest
```

## Usage

```sh
anonseed <seed> [args...]
anonseed --help
anonseed --version
```

Seed the `pi` tool's config into an anonymized identity, wiring the local model endpoint it reaches directly:

```sh
sudo anonseed pi --endpoint 127.0.0.1:11434
```

anonseed self-elevates for the root-owned `/etc/anonctl` write, so it re-execs under `sudo` if not already root.

### The `pi` seed

Given the local model endpoint's `host:port` (via `--endpoint`, or asked interactively if omitted), the `pi` seed probes the endpoint's live `/v1/models`, reads the endpoint-matched provider from your own `~/.pi/agent/models.json`, imports every discovered model (asking which is the default when there is more than one), and synthesizes a `models.json` + `settings.json` into the target home's `~/.pi/agent/`, declaring the `--allow` exception for that endpoint. It refuses to seed a real-looking API key into an anonymized home (a jailed identity carrying the operator's credentials still authenticates as the operator); pass `--force-allow-local-llm-api-key` to override. It also wires webveil (SearXNG) web search by default (`--no-webveil` to disable), declaring the `npm:pi-webveil` extension in the seeded settings. When no SearXNG is detected it asks whether to proceed without it, recheck (after you install one), or wire the install default anyway, pointing at the SearXNG install guide.

The substrate is chosen with `--target {anonctl,anonbox}`; with no `--target`, anonseed detects the present substrates and asks which to seed.

## Release

Releases are cut by pushing a `vX.Y.Z` tag; a GitHub Actions workflow runs goreleaser to cross-compile the Linux binaries (amd64, arm64, armv7, armv6), build checksums, and publish them to the GitHub Release.

## License

[AGPL-3.0-only](./LICENSE).
