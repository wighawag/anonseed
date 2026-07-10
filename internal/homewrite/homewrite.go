// Package homewrite is anonseed's shared seeding surface: it resolves a target
// identity into a (home, account) pair, and lands a set of files into that home
// through anoncore's seedhome, so anonseed NEVER re-implements the file-level
// credential-shedding. Because the write is anoncore's seedhome.Seed (imported,
// not vendored), this hardening CANNOT drift from anonctl's: it is the SAME code.
//
// Two responsibilities, both consumed by the substrate appliers (e.g. the
// anonctl target, a separate task):
//
//   - ResolveDefaultHome / ResolveAccountHome: turn a chosen sub-target into the
//     home path to write and the account name to chown to. Two sub-targets: the
//     box-wide default-home (a template dir under anonctl's base dir, owned by the
//     default `anon` account) and a specific already-provisioned anon account's
//     home (looked up from passwd through anoncore's provision.AccountHome). The
//     account-name vocabulary is anoncore's account.ResolveAccount.
//
//   - Write: materialise a SeedPlan's Files under a temporary template dir and
//     hand it to seedhome.Seed, which strips setuid/setgid/sticky bits, refuses
//     symlinks, writes mode-700, and does an ATOMIC collision check (writes
//     nothing if any target collides, unless force). Write is create-only by
//     default (force == false), consistent with anonctl's create-only `add`.
//
// This package owns NEITHER the concrete `/etc/anonctl` base path nor the
// defaults.json `"allow"` merge: those are the anonctl target applier's job
// (task anonctl-target-file-conventions), which supplies the base dir here. This
// surface stays substrate-shaped-but-substrate-agnostic so every seed type and
// every target reuses the SAME safe write.
package homewrite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wighawag/anoncore/account"
	"github.com/wighawag/anoncore/provision"
	"github.com/wighawag/anoncore/seedhome"
	"github.com/wighawag/anonseed/internal/seed"
)

// Runner is the command-execution seam seedhome uses for its chown step. It is
// re-declared here (identical to seedhome.Runner / provision.Runner) so callers
// and tests depend on this package's surface, not anoncore's directly; production
// wires anoncore's provision.ExecRunner, tests inject a fake so no root/real
// chown is needed.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
}

// defaultHomeDir is the subdirectory of anonctl's base dir that holds the
// box-wide default-home template (the seed template every fresh `anonctl add`
// inherits). The concrete base dir is supplied by the caller; only this leaf
// name is anonctl's fixed convention.
const defaultHomeDir = "default-home"

// Identity is a resolved target identity: the home directory to write into and
// the account name every written path is chowned to.
type Identity struct {
	// Home is the absolute directory the seed's files land under.
	Home string

	// Account is the Unix account name (anoncore's account vocabulary) that owns
	// the seeded paths, or EMPTY for a root-owned write with NO account chown.
	//
	// A named account is the second sub-target: a specific already-provisioned
	// anon account's home, chowned to that account (which MUST exist). An EMPTY
	// Account is the box-wide default-home TEMPLATE: it is a root-owned skeleton
	// under /etc/anonctl/default-home/ that anonctl's own `add` later copies INTO a
	// fresh account's home (doing the per-account chown at that point, mirroring
	// anonctl's documented `sudo cp -r <src>/. /etc/anonctl/default-home/`). Seeding
	// the template must NOT require the `anon` account to exist, so an empty Account
	// skips the chown entirely. See ResolveDefaultHome / ResolveAccountHome.
	Account string
}

// ResolveDefaultHome resolves the FIRST sub-target: anonctl's box-wide
// default-home template under baseDir (e.g. baseDir=/etc/anonctl gives
// /etc/anonctl/default-home). The template is owned by the default `anon`
// account. baseDir is a caller-supplied seam (the anonctl target applier owns the
// concrete /etc/anonctl default and points it at a temp dir in tests), so this
// surface never hardcodes a system path.
func ResolveDefaultHome(baseDir string) Identity {
	return Identity{
		Home: filepath.Join(baseDir, defaultHomeDir),
		// EMPTY account: the default-home is a TEMPLATE, not a live account home. It is
		// written root-owned (no account chown), exactly as anonctl populates it with a
		// plain root `cp -r`; anonctl's `add` does the per-account chown when it copies
		// this template into a fresh account's home. Seeding it must therefore NOT
		// require the `anon` account to exist on the box.
		Account: "",
	}
}

// ResolveAccountHome resolves the SECOND sub-target: a specific already-
// provisioned anon account's home. The name is mapped through anoncore's
// account.ResolveAccount (“ / `anon` -> `anon`, `work` -> `anon-work`, an
// already-prefixed `anon-work` stays as-is), and the home directory is read from
// passwd via anoncore's provision.AccountHome over the SAME Runner seam (so tests
// drive it with a fake getent and no real account is required).
func ResolveAccountHome(ctx context.Context, r Runner, name string) (Identity, error) {
	acct := account.ResolveAccount(name)
	home, err := provision.AccountHome(ctx, r, acct)
	if err != nil {
		return Identity{}, fmt.Errorf("resolve home for account %q: %w", acct, err)
	}
	return Identity{Home: home, Account: acct}, nil
}

// Write lands a SeedPlan's files into a resolved identity's home through
// seedhome.Seed. It materialises each FileToWrite under a private temporary
// template directory (preserving the home-relative directory structure), then
// delegates the actual write to seedhome, which provides the load-bearing
// semantics: setuid/setgid/sticky stripped, symlinks refused, mode-700, and an
// ATOMIC collision check (nothing is written if any target path already exists,
// unless force). Create-only by default: force must be explicit to overwrite,
// consistent with anonctl's create-only `add`.
//
// The temporary template dir is always removed before Write returns, so no
// scratch state leaks onto disk. The seedhome.Result (files copied, any
// overwritten paths) is returned for the caller to report.
func Write(ctx context.Context, r Runner, id Identity, files []seed.FileToWrite, force bool) (seedhome.Result, error) {
	tmpl, err := os.MkdirTemp("", "anonseed-seed-*")
	if err != nil {
		return seedhome.Result{}, fmt.Errorf("create seed template dir: %w", err)
	}
	defer os.RemoveAll(tmpl)

	if err := materialise(tmpl, files); err != nil {
		return seedhome.Result{}, err
	}

	// A named account is chowned to; an EMPTY account is a root-owned template write
	// (the box-wide default-home), so the chown is suppressed. seedhome always issues
	// a `chown <account>:<account>` per path, so for the template case we pass a
	// Runner that drops the chown while leaving every OTHER seedhome guarantee (atomic
	// collision check, setuid/sticky strip, symlink refusal, mode-700) intact. This is
	// what lets `anonseed pi` seed the default-home on a box where `anon` does not yet
	// exist. seedhome's `account` arg is then irrelevant (its only use is the chown we
	// drop), so "root" is passed purely as a non-empty placeholder for its messages.
	runner, account := r, id.Account
	if account == "" {
		runner = noChownRunner{r}
		account = "root"
	}

	return seedhome.Seed(ctx, runner, tmpl, id.Home, account, force)
}

// noChownRunner wraps a Runner and DROPS `chown` invocations (returning success
// without executing them), forwarding everything else. It is how Write does a
// root-owned template seed: seedhome unconditionally chowns each seeded path, but
// the default-home template must stay root-owned and must not require any account
// to exist. Only the chown is suppressed; seedhome's copy, collision, setuid-strip
// and symlink guarantees are untouched (they are pure filesystem work in
// seedhome, not Runner calls).
type noChownRunner struct{ inner Runner }

func (n noChownRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	if name == "chown" {
		return "", "", nil
	}
	return n.inner.Run(ctx, name, args...)
}

// materialise writes each file's content into the template dir at its
// home-relative path, creating parent directories as needed. Every file is
// written mode 0600: the seeded home is mode-700 territory and a seed's config
// files carry no executable or setuid intent. seedhome re-asserts the safe mode
// and strips any setuid/setgid/sticky bits regardless, so this is a defence in
// depth, not the guard itself.
//
// A path that is absolute or escapes the template (via `..`) is refused: a
// SeedPlan declares home-RELATIVE paths, and a traversal would let a file land
// outside the home. seedhome would also refuse to escape the home, but catching
// it here keeps the template itself clean.
func materialise(tmpl string, files []seed.FileToWrite) error {
	for _, f := range files {
		if err := checkRelPath(f.Path); err != nil {
			return err
		}
		dst := filepath.Join(tmpl, f.Path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return fmt.Errorf("create template dir for %q: %w", f.Path, err)
		}
		if err := os.WriteFile(dst, []byte(f.Content), 0o600); err != nil {
			return fmt.Errorf("write template file %q: %w", f.Path, err)
		}
	}
	return nil
}

// checkRelPath refuses an absolute path or one that escapes its root via `..`, so
// a SeedPlan's declared home-relative path cannot land a file outside the home.
func checkRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("seed file has an empty path")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("seed file path %q is absolute; SeedPlan paths must be home-relative", p)
	}
	// Clean collapses interior traversals, so a path escapes iff it cleans to `..`
	// or begins with a leading `../`. (A bare `.` writes nothing useful either.)
	clean := filepath.Clean(p)
	sep := string(filepath.Separator)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+sep) {
		return fmt.Errorf("seed file path %q escapes the home directory", p)
	}
	return nil
}
