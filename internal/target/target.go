// Package target owns anonseed's `--target` substrate axis: the axis ORTHOGONAL
// to seed-type (a seed is chosen by subcommand, `anonseed pi`; a substrate is
// chosen here). It is the seam between the CLI's parsed `--target` flag and the
// driver + substrate appliers, so a caller wiring a seed does not re-implement
// substrate selection, detection, or the per-target fan-out.
//
// Four responsibilities, all behind seams so the whole axis is testable without
// a real environment:
//
//   - Parse: turn an explicit `--target {anonctl,anonbox}` value into a
//     seed.Target, failing LOUDLY on an unknown value (never a silent default).
//   - Detector: report which substrates are PRESENT on the box (is anonctl
//     installed? is anonbox?). Production sniffs the real environment; tests
//     inject a fake so present/absent is deterministic.
//   - Select: the DEFAULT (no `--target`) selection. It DETECTS the present
//     substrates, then ASKS the operator which to seed (never a silent
//     auto-pick), and may return SEVERAL targets to seed as many applicable
//     substrates as are present. An explicit `--target` bypasses detection +
//     the prompt and selects exactly that one.
//   - Run: the fan-out. Given a seed and a set of selected targets, it drives
//     each target through the pure driver (seed.Run, which SKIPS a target the
//     seed does not declare in its Targets()) and routes each produced plan into
//     the matching substrate applier (anonctl now, anonbox stub). A skip is a
//     clean, non-fatal, reported outcome, NOT an error and NOT a mis-seed.
//
// # The two "not this substrate" outcomes, kept distinct
//
// There are two different reasons a selected target seeds nothing, and they are
// deliberately NOT conflated:
//
//   - the seed does not DECLARE the target (Targets() omits it): a SKIP, decided
//     by the driver (seed.Run), non-fatal and reported (Outcome.Skipped). The
//     target was present/requested but this seed does not apply to it.
//   - the substrate's applier cannot deliver yet (anonbox is a stub): an ERROR
//     from the applier (anonbox.ErrNotYetAvailable), surfaced on the Outcome.
//
// Reusing "skip" for the second would muddle "this seed does not apply here" with
// "this substrate is not built yet" (see anonbox package doc); this package keeps
// the skip decision UPSTREAM (the driver) of the apply decision (the applier).
package target

import (
	"context"
	"fmt"
	"sort"

	"github.com/wighawag/anonseed/internal/anonbox"
	"github.com/wighawag/anonseed/internal/anonctl"
	"github.com/wighawag/anonseed/internal/seed"
)

// Known lists every substrate the `--target` flag accepts, in a stable order for
// help text and prompts. It is derived from the seed package's Target constants,
// so a new substrate is added in one place (seed) and surfaces here.
var Known = []seed.Target{seed.TargetAnonctl, seed.TargetAnonbox}

// Parse turns an explicit `--target` flag value into a seed.Target, accepting
// ONLY a known substrate name. An unknown value is a LOUD error naming the bad
// value and the accepted set, so a typo (`--target anonctrl`) fails fast rather
// than silently falling back to a default. An empty value is rejected here too;
// the empty (no-flag) case is the DEFAULT path (Select), not a Parse input.
func Parse(value string) (seed.Target, error) {
	for _, k := range Known {
		if value == string(k) {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown --target %q: expected one of %s", value, joinTargets(Known))
}

// Detector reports which substrates are PRESENT on the box, so the default
// (no-flag) selection can offer the operator exactly the ones that are installed.
// It is the environment-sniffing SEAM: production wires a real detector (does
// /etc/anonctl exist? is the anonbox tool on PATH?); tests inject a fake that
// returns a fixed present-set, so detect-then-ask is driven deterministically
// without the real environment.
type Detector interface {
	// Detect returns the substrates present on the box, a SUBSET of Known. Order
	// is not significant (Select normalises + de-duplicates); an empty result
	// means no substrate is present.
	Detect(ctx context.Context) []seed.Target
}

// DetectorFunc adapts a plain function to the Detector interface, so a caller (or
// a test) can supply detection without declaring a type.
type DetectorFunc func(ctx context.Context) []seed.Target

// Detect implements Detector.
func (f DetectorFunc) Detect(ctx context.Context) []seed.Target { return f(ctx) }

// Prompt asks the operator WHICH of the present substrates to seed. It receives
// the detected present set (already normalised, never empty when called) and
// returns the chosen subset. It is the interactivity SEAM: production shows a
// real prompt; tests script the choice. Returning an empty slice means "seed
// none" (the operator declined); returning an error aborts selection.
//
// The prompt exists so the default NEVER silently auto-picks: even when exactly
// one substrate is present, the operator is asked (they may want to seed none).
type Prompt func(present []seed.Target) ([]seed.Target, error)

// Select resolves which target(s) to seed WHEN NO explicit `--target` was given:
// it DETECTS the present substrates, then ASKS the operator via prompt which to
// seed (never a silent auto-pick). It may return SEVERAL targets, to seed as many
// applicable substrates as are present. An explicit `--target` does NOT come
// through here (the caller uses Parse for that and skips detection + the prompt).
//
// When NO substrate is detected, Select returns a clear error WITHOUT prompting
// (there is nothing to offer). The returned targets are normalised to Known order
// and de-duplicated, and every returned value is one the detector reported
// present (a prompt cannot conjure an absent substrate).
func Select(ctx context.Context, det Detector, prompt Prompt) ([]seed.Target, error) {
	if det == nil || prompt == nil {
		return nil, fmt.Errorf("target.Select needs a Detector and a Prompt")
	}

	present := normalise(det.Detect(ctx))
	if len(present) == 0 {
		return nil, fmt.Errorf("no substrate detected: neither %s is present on this box (install one, or pass --target explicitly)", joinTargets(Known))
	}

	chosen, err := prompt(present)
	if err != nil {
		return nil, fmt.Errorf("target selection aborted: %w", err)
	}

	// Constrain the operator's choice to what was actually detected present, so a
	// prompt implementation cannot return an absent substrate (defence in depth;
	// a well-behaved prompt only offers `present`).
	presentSet := make(map[seed.Target]bool, len(present))
	for _, t := range present {
		presentSet[t] = true
	}
	out := make([]seed.Target, 0, len(chosen))
	for _, t := range chosen {
		if !presentSet[t] {
			return nil, fmt.Errorf("target selection chose %q, which was not detected present", t)
		}
		out = append(out, t)
	}
	return normalise(out), nil
}

// Outcome is the result of driving a seed against ONE selected target. Exactly
// one of three things happened, distinguished so a caller can report each
// precisely:
//
//   - Skipped == true: the seed does not DECLARE this target in its Targets(),
//     so the driver skipped it (non-fatal, not a mis-seed). Applied is false.
//   - Err != nil: the seed's Plan failed, OR the substrate applier could not
//     deliver (e.g. anonbox.ErrNotYetAvailable). The target was applicable and
//     attempted, but the attempt errored.
//   - Applied == true, Err == nil, Skipped == false: the plan was produced and
//     handed to the substrate applier successfully.
type Outcome struct {
	// Target is the substrate this outcome is for.
	Target seed.Target

	// Skipped is true when the seed does not declare Target (driver skip). When
	// true, Applied is false and Err is nil.
	Skipped bool

	// Applied is true when the plan was produced and the applier delivered it
	// without error.
	Applied bool

	// Err is non-nil when the seed's Plan failed or the applier could not deliver
	// (matchable, e.g. errors.Is(o.Err, anonbox.ErrNotYetAvailable)).
	Err error
}

// Applier delivers a produced seed.SeedPlan onto ONE substrate. It is the seam
// between the driver's per-target plan and the concrete substrate appliers, so
// Run does not hardcode which applier a target routes to and tests can substitute
// a recording applier. Production wires DefaultAppliers (anonctl + anonbox).
type Applier func(ctx context.Context, plan seed.SeedPlan) error

// Run is the fan-out: it drives seed s against EACH selected target through the
// pure driver (seed.Run) and routes each produced plan into the matching
// substrate applier from appliers. It returns one Outcome per target, in the
// input order, so a caller can report exactly what happened to each substrate
// (applied / skipped / errored) without the fan-out deciding policy.
//
// The driver's skip is honoured FIRST: a target the seed does not declare in its
// Targets() yields a Skipped outcome and its applier is never called (no
// mis-seed). A declared target's plan is handed to appliers[target]; a target
// with no registered applier is an error outcome (a selection the caller cannot
// deliver). Run does not stop on the first error: every selected target gets an
// Outcome, so a multi-target fan-out reports the whole picture (e.g. anonctl
// applied AND anonbox not-yet-available), rather than aborting midway.
func Run(ctx context.Context, s seed.Seed, opts seed.Options, targets []seed.Target, appliers map[seed.Target]Applier) []Outcome {
	outcomes := make([]Outcome, 0, len(targets))
	for _, t := range targets {
		res, err := seed.Run(ctx, s, opts, t)
		if err != nil {
			outcomes = append(outcomes, Outcome{Target: t, Err: err})
			continue
		}
		if res.Skipped {
			outcomes = append(outcomes, Outcome{Target: t, Skipped: true})
			continue
		}
		apply, ok := appliers[t]
		if !ok {
			outcomes = append(outcomes, Outcome{Target: t, Err: fmt.Errorf("no applier registered for target %q", t)})
			continue
		}
		if err := apply(ctx, res.Plan); err != nil {
			outcomes = append(outcomes, Outcome{Target: t, Err: err})
			continue
		}
		outcomes = append(outcomes, Outcome{Target: t, Applied: true})
	}
	return outcomes
}

// DefaultAppliers wires the production substrate appliers: the anonctl applier
// (buildable now) lands the plan onto anonctl's box-wide default-home + defaults
// via the given Applier, and the anonbox applier is the loud not-yet-available
// stub. The anonctl applier needs a base dir + Runner + sub-target choice, which
// the CLI owns, so it is passed in as anonctlApply rather than constructed here
// (this package stays free of the /etc/anonctl base path and the Runner seam).
func DefaultAppliers(anonctlApply Applier) map[seed.Target]Applier {
	return map[seed.Target]Applier{
		seed.TargetAnonctl: anonctlApply,
		seed.TargetAnonbox: func(ctx context.Context, plan seed.SeedPlan) error {
			return anonbox.Apply(ctx, plan)
		},
	}
}

// AnonctlDefaultHomeApplier adapts the anonctl applier's ApplyDefaultHome (the
// box-wide default-home sub-target, the common seed target) to the target.Applier
// seam, discarding the rich anonctl.Result (the fan-out only needs success or
// error; a caller wanting the detailed result calls the applier directly). It
// exists so the CLI can wire the anonctl target with one call.
func AnonctlDefaultHomeApplier(a anonctl.Applier, force bool) Applier {
	return func(ctx context.Context, plan seed.SeedPlan) error {
		_, err := a.ApplyDefaultHome(ctx, plan, force)
		return err
	}
}

// normalise sorts a target slice into Known order and removes duplicates and any
// value outside Known, so Select + Detect results are stable and clean regardless
// of the order a detector or prompt returned them in.
func normalise(targets []seed.Target) []seed.Target {
	rank := make(map[seed.Target]int, len(Known))
	for i, k := range Known {
		rank[k] = i
	}
	seen := make(map[seed.Target]bool, len(targets))
	out := make([]seed.Target, 0, len(targets))
	for _, t := range targets {
		if _, known := rank[t]; !known {
			continue
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i]] < rank[out[j]] })
	return out
}

// joinTargets renders a target set as a comma-separated `{a, b}` for error /
// help text.
func joinTargets(targets []seed.Target) string {
	s := "{"
	for i, t := range targets {
		if i > 0 {
			s += ", "
		}
		s += string(t)
	}
	return s + "}"
}
