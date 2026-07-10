package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/wighawag/anonseed/internal/anonctl"
	"github.com/wighawag/anonseed/internal/seed"
	"github.com/wighawag/anonseed/internal/target"
)

// piHandler is the `pi` seed's CLI handler: it owns the pi seed's argv (its
// --endpoint + the api-key force flag) AND the `--target` substrate axis (the
// flag + interactive detect-then-ask default + the multi-target fan-out, task
// target-flag-and-detection). It resolves the pi seed UPSTREAM (the interactive
// probe/pick), then drives it through the target axis: an explicit --target
// selects one substrate; no --target detects the present substrates and ASKS
// which to seed (never a silent auto-pick), possibly fanning out to several.
//
// The target axis itself lives in internal/target (reusable across seed types);
// this handler is the wiring that reads the flag, resolves the seed, and reports
// the per-target outcomes. Every impure edge (resolving the pi seed, detecting
// present substrates, prompting the operator, applying to anonctl) is behind an
// injectable seam so this wiring is testable without a real endpoint, a real box,
// or a real /etc/anonctl write.
type piHandler struct {
	// resolveSeed builds the pi seed.Seed from the parsed flags (the interactive
	// probe/pick + the api-key guard live in piseed.Resolve, behind this seam).
	// Production wires resolvePiSeed; cli tests inject a fake seed.
	resolveSeed func(ctx context.Context, endpoint string, force bool, stdout, stderr io.Writer) (seed.Seed, error)

	// detector reports the present substrates for the default (no --target) path.
	// Production wires target.EnvDetector; tests fake present/absent.
	detector target.Detector

	// prompt asks the operator which present substrates to seed (the detect-then-ask
	// interactivity). Production wires an interactive prompt; tests script it.
	prompt target.Prompt

	// anonctlApply lands a produced plan onto the anonctl substrate (its base dir +
	// Runner + sub-target). Production wires the real anonctl applier; tests record.
	anonctlApply target.Applier
}

func (piHandler) Summary() string {
	return "seed the pi tool's config into an anonymized identity"
}

// Run parses the pi seed's flags, resolves the seed, resolves the target(s), and
// fans out. It returns a process exit code: 0 when every selected target either
// applied or was cleanly skipped, non-zero when any target errored (or flag/seed
// resolution failed).
func (h piHandler) Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pi", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		targetFlag = fs.String("target", "", "substrate to seed into {anonctl,anonbox}; empty detects present substrates and asks")
		endpoint   = fs.String("endpoint", "", "the local model endpoint host:port the seeded pi reaches directly")
		force      = fs.Bool("force-allow-local-llm-api-key", false, "seed a real-looking apiKey anyway (normally refused; a local model ignores its key)")
	)
	if err := fs.Parse(args); err != nil {
		return 2 // flag package already printed the error to stderr.
	}
	if strings.TrimSpace(*endpoint) == "" {
		fmt.Fprintln(stderr, "anonseed pi: --endpoint host:port is required (the local model endpoint to wire).")
		return 2
	}

	ctx := context.Background()

	// UPSTREAM: resolve the pi seed (interactive probe/pick + api-key guard). A
	// resolution failure (e.g. a refused real apiKey) aborts before any target work.
	s, err := h.resolveSeed(ctx, *endpoint, *force, stdout, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "anonseed pi: %v\n", err)
		return 1
	}

	// Resolve which target(s) to seed: an explicit --target selects exactly one
	// (an unknown value fails loudly); no flag detects the present substrates and
	// asks the operator (never a silent auto-pick).
	targets, err := h.resolveTargets(ctx, *targetFlag, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "anonseed pi: %v\n", err)
		return 1
	}
	if len(targets) == 0 {
		fmt.Fprintln(stdout, "anonseed pi: no target selected; nothing seeded.")
		return 0
	}

	// Fan out: drive the seed against each target through the driver (which skips a
	// target the seed does not declare) and route each plan into its applier.
	appliers := target.DefaultAppliers(h.anonctlApply)
	outcomes := target.Run(ctx, s, seed.Options{Endpoint: *endpoint}, targets, appliers)

	return reportOutcomes(outcomes, stdout, stderr)
}

// resolveTargets turns the --target flag into the set of substrates to seed. An
// explicit value is parsed to exactly one target (an unknown value is a loud
// error). An empty value is the DEFAULT: detect the present substrates and ask
// the operator which to seed (target.Select), which may return several.
func (h piHandler) resolveTargets(ctx context.Context, targetFlag string, stderr io.Writer) ([]seed.Target, error) {
	if strings.TrimSpace(targetFlag) != "" {
		t, err := target.Parse(targetFlag)
		if err != nil {
			return nil, err
		}
		return []seed.Target{t}, nil
	}
	return target.Select(ctx, h.detector, h.prompt)
}

// reportOutcomes prints one line per target outcome (applied / skipped / errored)
// and returns 0 iff no target errored. A skip is reported as an informational,
// non-fatal line (the seed does not support that substrate); an applier error is
// a loud line and forces a non-zero exit.
func reportOutcomes(outcomes []target.Outcome, stdout, stderr io.Writer) int {
	code := 0
	for _, o := range outcomes {
		switch {
		case o.Err != nil:
			fmt.Fprintf(stderr, "anonseed pi: target %q failed: %v\n", o.Target, o.Err)
			code = 1
		case o.Skipped:
			fmt.Fprintf(stdout, "anonseed pi: target %q skipped (the pi seed does not support this substrate).\n", o.Target)
		case o.Applied:
			fmt.Fprintf(stdout, "anonseed pi: seeded target %q.\n", o.Target)
		}
	}
	return code
}

// newPiHandler builds the production pi handler, wiring the real seams: the
// interactive seed resolution, the environment detector, an interactive prompt,
// and the anonctl applier (box-wide default-home sub-target, create-only). It is
// the one place the production impure edges are assembled, so the handler struct
// stays seam-injectable for tests.
func newPiHandler() piHandler {
	return piHandler{
		resolveSeed:  resolvePiSeed,
		detector:     target.EnvDetector{},
		prompt:       interactiveTargetPrompt,
		anonctlApply: target.AnonctlDefaultHomeApplier(anonctl.Applier{Runner: provisionExecRunner()}, false),
	}
}
