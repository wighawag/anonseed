// Package anonctl is the anonctl substrate applier: the DOWNSTREAM half of the
// anonctl target axis that consumes a seed.SeedPlan and lands it onto anonctl's
// ON-DISK FILE CONVENTIONS. It is buildable NOW (unlike the anonbox applier,
// which stubs until anonbox exists), because the anonctl substrate is
// home-on-host with file conventions anonctl already ships.
//
// # The seam between anonseed and anonctl is the FILE LAYOUT, not an import
//
// anonseed writes anonctl's conventions DIRECTLY and imports anoncore ONLY; it
// does NOT import anonctl. anonctl's own logic lives in its Go-INTERNAL
// `internal/defaults` (un-importable), and that is fine: the contract between the
// two tools is deliberately the dependency-free file layout, which anonctl
// documents as `cp`-able. The two conventions this applier writes, mirrored from
// anonctl's real internal/defaults VERBATIM:
//
//   - the box-wide default-home template dir `<baseDir>/default-home/` (a
//     directory-exists convention: its PRESENCE switches on `add`-time home
//     seeding), OR a specific already-provisioned account's home;
//   - the box-wide defaults file `<baseDir>/defaults.json`, shape
//     `{"allow": ["IP|CIDR:port", ...]}` (config key `"allow"`, stored RAW, port
//     mandatory), which `add` applies when given no `--allow`.
//
// # Two sub-targets (the home axis)
//
// ApplyDefaultHome seeds the box-wide default-home template every fresh
// `anonctl add` inherits (spec story 11); ApplyAccountHome seeds a SPECIFIC
// already-provisioned anon account's home (spec story 13). Both land the plan's
// Files through the SAME safe-write surface (internal/homewrite -> anoncore
// seedhome: setuid/setgid/sticky stripped, symlinks refused, mode-700, an ATOMIC
// create-only collision check), and merge the plan's Exceptions into the SAME
// defaults.json. The only difference is which home the files land in.
//
// # The base-dir seam (shared-write isolation)
//
// `/etc/anonctl` is a SHARED system location, so the base directory is behind an
// override (Applier.BaseDir), exactly as anonctl's own defaults.Store.BaseDir is:
// production uses DefaultBaseDir; tests point it at a scratch temp dir so no test
// ever touches the real `/etc/anonctl`. A zero BaseDir means DefaultBaseDir, so a
// production caller need not spell the path.
//
// # defaults.json merge is create-if-absent + preserve (never clobber)
//
// Merging an Exception into defaults.json READS any existing file first, APPENDS
// only the new (non-duplicate) allow values, and writes the whole record back, so
// an operator's hand-added exemptions are preserved and re-running the seed is
// idempotent (no duplicate entries). A missing file is created; a present file is
// preserved. Each raw Exception.Allow passes the fail-fast allowguard pre-check
// FIRST (a bad value fails early with a clear message, before any write), though
// anonctl re-validates authoritatively at `add` time (see internal/allowguard and
// docs/adr/0002). Unlike the home write, defaults.json is a root-owned `/etc`
// file, not a chowned account home, so it is written directly at mode 0644
// (matching anonctl's own defaults.json fixtures), NOT through seedhome.
package anonctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wighawag/anoncore/seedhome"
	"github.com/wighawag/anonseed/internal/allowguard"
	"github.com/wighawag/anonseed/internal/homewrite"
	"github.com/wighawag/anonseed/internal/seed"
)

// Target is the substrate this applier delivers onto, re-exported from the seed
// package so a caller wiring the target axis names the anonctl target through
// this applier's own surface. It is exactly seed.TargetAnonctl.
const Target = seed.TargetAnonctl

// DefaultBaseDir is the real anonctl config root (`/etc/anonctl`), holding both
// defaults.json and the default-home/ template. It mirrors anonctl's own
// defaults.DefaultBaseDir VERBATIM. A zero Applier.BaseDir means this path; tests
// override it so no test reads or writes the real `/etc/anonctl`.
const DefaultBaseDir = "/etc/anonctl"

// defaultsFile is the box-wide defaults record's name under BaseDir, mirroring
// anonctl's internal/defaults defaultsFile. Its shape is `{"allow": [...]}`.
const defaultsFile = "defaults.json"

// defaultsFileMode is the mode the defaults.json file is written with. It is a
// root-owned `/etc` config file (readable by the account that `add` runs as),
// matching the 0644 anonctl's own defaults_test.go writes its fixtures with.
const defaultsFileMode = 0o644

// Runner is the command-execution seam threaded through to homewrite (for the
// chown seedhome issues and, for a named account, the passwd lookup). It is the
// same shape as homewrite.Runner / anoncore's provision.Runner; production wires
// anoncore's provision.ExecRunner, tests inject a fake so no root or real chown
// is needed.
type Runner = homewrite.Runner

// Applier lands a SeedPlan onto the anonctl substrate under a chosen base dir.
// The base dir is the shared-write isolation seam: production leaves it empty
// (DefaultBaseDir), tests point it at a temp dir. The Runner is the same seam
// homewrite uses for the account chown / passwd lookup.
type Applier struct {
	// BaseDir is the anonctl config root holding defaults.json and default-home/.
	// Empty means DefaultBaseDir, so a zero Applier still targets the real path.
	BaseDir string

	// Runner is the command seam for the chown seedhome issues and the passwd
	// lookup ApplyAccountHome does. Required (a nil Runner would panic on write).
	Runner Runner
}

// Result is the outcome of applying a plan to the anonctl substrate: the home
// write result (files copied / any overwritten) and the allow values that were
// newly ADDED to defaults.json (the merge is de-duplicating, so an already-present
// value is not re-added and does not appear here).
type Result struct {
	// Home is the seedhome result for the files landed into the target home.
	Home seedhome.Result

	// AllowAdded are the raw `IP|CIDR:port` values newly appended to defaults.json
	// (existing ones preserved but not re-listed here). Empty when the plan had no
	// exceptions or all were already present.
	AllowAdded []string
}

func (a Applier) baseDir() string {
	if a.BaseDir == "" {
		return DefaultBaseDir
	}
	return a.BaseDir
}

// ApplyDefaultHome applies a plan to the FIRST sub-target: anonctl's box-wide
// default-home template under the base dir (`<baseDir>/default-home`, owned by the
// default `anon` account), the template every fresh `anonctl add` inherits. It
// lands the plan's Files there and merges its Exceptions into defaults.json. force
// controls only the create-only home write (an existing seeded file collides
// loudly unless force); the defaults.json merge is always non-destructive.
func (a Applier) ApplyDefaultHome(ctx context.Context, plan seed.SeedPlan, force bool) (Result, error) {
	id := homewrite.ResolveDefaultHome(a.baseDir())
	return a.apply(ctx, id, plan, force)
}

// ApplyAccountHome applies a plan to the SECOND sub-target: a SPECIFIC already-
// provisioned anon account's home. The name is resolved through anoncore's
// account vocabulary and a passwd lookup over the Runner seam (homewrite.
// ResolveAccountHome), so an existing account is wired up without re-seeding the
// box-wide template. It lands the plan's Files into that home and merges its
// Exceptions into defaults.json.
func (a Applier) ApplyAccountHome(ctx context.Context, name string, plan seed.SeedPlan, force bool) (Result, error) {
	id, err := homewrite.ResolveAccountHome(ctx, a.Runner, name)
	if err != nil {
		return Result{}, err
	}
	return a.apply(ctx, id, plan, force)
}

// apply is the shared body both sub-targets converge on: validate every exception
// FIRST (fail fast before any write), land the files, then merge the exceptions.
// The order is deliberate: a bad `--allow` value aborts the WHOLE apply before a
// single file is written, so a plan is never half-applied because of an exemption
// typo.
func (a Applier) apply(ctx context.Context, id homewrite.Identity, plan seed.SeedPlan, force bool) (Result, error) {
	// Fail-fast pre-check of EVERY exception before touching disk: allowguard.Parse
	// rejects a public / :53 / port-omitted / hostname value loudly and early. This
	// is UX (anonctl re-validates authoritatively at `add`), but doing it up front
	// means a bad exemption never leaves a half-written home behind.
	for _, ex := range plan.Exceptions {
		if _, err := allowguard.Parse(ex.Allow); err != nil {
			return Result{}, fmt.Errorf("exception %q rejected by the --allow guardrail: %w", ex.Allow, err)
		}
	}

	// Ensure the target home directory exists before seeding. seedhome creates any
	// directory COMPONENTS a template path carries, but not the home ROOT itself, so
	// a plan whose files are all top-level (e.g. "cfg", no intermediate dir) would
	// otherwise fail to write into a not-yet-existing home. For the box-wide
	// default-home this is also the directory-exists convention anonseed is meant to
	// establish (anonctl's `add` treats the presence of default-home/ as the switch;
	// see anonctl internal/defaults). A named account's home already exists (it was
	// provisioned), so MkdirAll is a harmless no-op there. Mode 0700 matches the
	// mode-700 home territory seedhome enforces for the files themselves.
	if err := os.MkdirAll(id.Home, 0o700); err != nil {
		return Result{}, fmt.Errorf("create target home %q: %w", id.Home, err)
	}

	homeRes, err := homewrite.Write(ctx, a.Runner, id, plan.Files, force)
	if err != nil {
		return Result{}, err
	}

	added, err := a.mergeExceptions(plan.Exceptions)
	if err != nil {
		return Result{Home: homeRes}, err
	}

	return Result{Home: homeRes, AllowAdded: added}, nil
}

// defaultsRecord mirrors anonctl's internal/defaults Defaults shape VERBATIM: the
// box-wide add-time defaults, carrying only the RAW allow list (a port is
// mandatory in each value). `omitempty` matches anonctl so an empty record
// round-trips to `{}` exactly as anonctl would read it. anonseed writes this shape
// directly; it does NOT import anonctl.
type defaultsRecord struct {
	Allow []string `json:"allow,omitempty"`
}

// mergeExceptions merges each exception's raw Allow value into defaults.json's
// "allow" list under the base dir, preserving any existing entries and adding no
// duplicates (idempotent re-seeding). A missing defaults.json is created; a
// present one is read, appended to, and written back whole. It returns the values
// NEWLY added (an already-present value is skipped and not returned). Exceptions
// have already passed the allowguard pre-check in apply, so this step only merges.
func (a Applier) mergeExceptions(exceptions []seed.Exception) ([]string, error) {
	if len(exceptions) == 0 {
		return nil, nil
	}

	path := filepath.Join(a.baseDir(), defaultsFile)

	rec, err := readDefaults(path)
	if err != nil {
		return nil, err
	}

	// Seed a membership set from the existing entries so an existing value (whether
	// an operator hand-added it or a prior seed run wrote it) is never duplicated.
	present := make(map[string]bool, len(rec.Allow))
	for _, v := range rec.Allow {
		present[v] = true
	}

	var added []string
	for _, ex := range exceptions {
		if present[ex.Allow] {
			continue
		}
		present[ex.Allow] = true
		rec.Allow = append(rec.Allow, ex.Allow)
		added = append(added, ex.Allow)
	}

	// Nothing new: leave the file exactly as it was (do not rewrite), so a no-op
	// re-seed does not churn the file's bytes or mtime.
	if len(added) == 0 {
		return nil, nil
	}

	if err := writeDefaults(path, rec); err != nil {
		return nil, err
	}
	return added, nil
}

// readDefaults loads the box-wide defaults from path. A MISSING file is a clean
// empty record (the common first-seed case), NOT an error, so the merge need not
// special-case absence. A present-but-corrupt file IS a loud error (never
// silently treated as empty and clobbered), so a hand-edit typo fails visibly
// rather than dropping the operator's existing exemptions. Mirrors anonctl
// defaults.Store.Read.
func readDefaults(path string) (defaultsRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultsRecord{}, nil
		}
		return defaultsRecord{}, fmt.Errorf("read defaults %q: %w", path, err)
	}
	var rec defaultsRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return defaultsRecord{}, fmt.Errorf("invalid defaults JSON %q (refusing to clobber it): %w", path, err)
	}
	return rec, nil
}

// writeDefaults marshals the record and writes it to path, creating the base dir
// if needed. It writes atomically (temp file + rename in the same dir) so a
// crash mid-write never leaves a half-written / corrupt defaults.json that the
// next read (or anonctl's `add`) would reject. The file is mode 0644 (a
// root-owned `/etc` config, readable by the account `add` runs as), matching
// anonctl's own defaults.json fixtures.
func writeDefaults(path string, rec defaultsRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal defaults: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create defaults dir %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".defaults-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp defaults file: %w", err)
	}
	tmpName := tmp.Name()
	// Clean up the temp file on any early return; a successful rename makes this a
	// no-op (the temp name no longer exists).
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp defaults file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp defaults file: %w", err)
	}
	if err := os.Chmod(tmpName, defaultsFileMode); err != nil {
		return fmt.Errorf("chmod temp defaults file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("finalise defaults %q: %w", path, err)
	}
	return nil
}
