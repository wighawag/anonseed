package seed

import "context"

// Result is the outcome of running one seed against one target. Exactly one of
// two things happened: either the seed applied to the target and produced a
// Plan (Skipped == false), or the seed did not declare the target and was
// SKIPPED (Skipped == true, Plan zero-valued). A skip is a normal, non-fatal
// outcome, distinct from an error: skipping is not mis-seeding.
type Result struct {
	// Seed is the seed-type name that was run.
	Seed string

	// Target is the substrate the seed was run against.
	Target Target

	// Skipped is true when the seed does not declare Target in its Targets(),
	// so nothing was planned. When true, Plan is the zero value.
	Skipped bool

	// Plan is the synthesised plan when Skipped is false; the zero SeedPlan when
	// Skipped is true.
	Plan SeedPlan
}

// Run drives a single seed against a single target: it consults the seed's
// declared Targets(), and either skips (target not declared) or calls the seed's
// pure Plan and returns the resulting SeedPlan.
//
// Run performs NO substrate I/O itself: applying the returned SeedPlan (writing
// files via anoncore seedhome, declaring the --allow exceptions) is the job of a
// separate substrate applier. Run is the seam between "which seed for which
// target" and "apply this plan". An error from the seed's Plan is propagated
// unchanged; a target the seed does not declare is a non-error skip.
func Run(ctx context.Context, s Seed, opts Options, target Target) (Result, error) {
	res := Result{Seed: s.Name(), Target: target}

	if !supportsTarget(s, target) {
		res.Skipped = true
		return res, nil
	}

	plan, err := s.Plan(ctx, opts, target)
	if err != nil {
		return Result{Seed: s.Name(), Target: target}, err
	}
	res.Plan = plan
	return res, nil
}

// supportsTarget reports whether s declares target in its Targets().
func supportsTarget(s Seed, target Target) bool {
	for _, t := range s.Targets() {
		if t == target {
			return true
		}
	}
	return false
}
