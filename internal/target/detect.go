// detect.go holds the PRODUCTION substrate detector: the real
// environment-sniffing behind the Detector seam. It is deliberately separate from
// target.go's pure axis logic so the fan-out + selection stay testable with a
// fake detector, while this file owns the one impure edge (does the box actually
// have anonctl / anonbox?).
package target

import (
	"context"
	"os"

	"github.com/wighawag/anonseed/internal/anonctl"
	"github.com/wighawag/anonseed/internal/seed"
)

// EnvDetector is the production Detector: it reports a substrate PRESENT by
// sniffing the real box. It is the only impure part of the target axis, kept
// behind the Detector seam so every selection/fan-out test uses a fake instead.
//
// Presence signals (deliberately cheap, filesystem-only, no exec):
//
//   - anonctl: its config root exists (anonctl.DefaultBaseDir, `/etc/anonctl`).
//     anonctl is the tool that OWNS that host state, so its presence is the
//     directory anonseed writes into existing. AnonctlBaseDir overrides the path
//     for a non-standard install; empty means the real default.
//   - anonbox: NOT detectable yet. anonbox does not exist (its applier is the
//     loud not-yet-available stub), so it is never reported present. When anonbox
//     ships, its presence signal is added here; until then reporting it present
//     would only route the operator into the stub's error.
type EnvDetector struct {
	// AnonctlBaseDir overrides the anonctl config-root path whose existence signals
	// anonctl is present. Empty means anonctl.DefaultBaseDir (`/etc/anonctl`), so a
	// zero EnvDetector sniffs the real path. A test points it at a scratch dir to
	// drive present/absent, though most tests inject a fake Detector instead.
	AnonctlBaseDir string
}

// Detect reports the substrates present on this box (a subset of Known). Today
// that is anonctl iff its config root exists; anonbox is never reported (it does
// not exist yet, see the type doc). The result is unordered; Select normalises it.
func (d EnvDetector) Detect(_ context.Context) []seed.Target {
	var present []seed.Target
	if dirExists(d.anonctlBaseDir()) {
		present = append(present, seed.TargetAnonctl)
	}
	return present
}

func (d EnvDetector) anonctlBaseDir() string {
	if d.AnonctlBaseDir == "" {
		return anonctl.DefaultBaseDir
	}
	return d.AnonctlBaseDir
}

// dirExists reports whether path exists and is a directory. Any stat error
// (not-exist, permission) is treated as absent: detection must never abort
// selection, and a probe that cannot see the path treats the substrate as not
// present (the operator can still force it with an explicit --target).
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
