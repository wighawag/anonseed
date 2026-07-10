package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/wighawag/anonseed/internal/seed"
	"github.com/wighawag/anonseed/internal/target"
)

// stubPiSeed is a trivial in-test seed the pi handler drives, declaring a chosen
// target set so the handler's fan-out + skip wiring is exercised without the real
// pi seed's interactive resolution.
type stubPiSeed struct {
	targets []seed.Target
}

func (stubPiSeed) Name() string { return "pi" }

func (s stubPiSeed) Targets() []seed.Target { return s.targets }

func (stubPiSeed) Plan(_ context.Context, _ seed.Options, _ seed.Target) (seed.SeedPlan, error) {
	return seed.SeedPlan{
		Files:      []seed.FileToWrite{{Path: ".pi/agent/models.json", Content: "{}"}},
		Exceptions: []seed.Exception{{Allow: "127.0.0.1:1234"}},
	}, nil
}

// recordingApplier records the plans it applied, so a test can assert the anonctl
// target was routed to it.
type recordingApplier struct{ got []seed.SeedPlan }

func (r *recordingApplier) apply(_ context.Context, plan seed.SeedPlan) error {
	r.got = append(r.got, plan)
	return nil
}

// newTestPiHandler builds a pi handler with every impure seam faked: the seed
// resolution returns the given stub seed, detection returns the given present
// set, the prompt returns the given choice, and the anonctl applier records. No
// real endpoint, box, or /etc/anonctl is touched.
func newTestPiHandler(s seed.Seed, present, chosen []seed.Target, ctl *recordingApplier) piHandler {
	return piHandler{
		resolveSeed: func(_ context.Context, _ string, _ bool, _, _ io.Writer) (seed.Seed, error) {
			return s, nil
		},
		detector:     target.DetectorFunc(func(context.Context) []seed.Target { return present }),
		prompt:       func([]seed.Target) ([]seed.Target, error) { return chosen, nil },
		anonctlApply: ctl.apply,
	}
}

// TestPiRequiresEndpoint: the pi handler refuses without --endpoint (a usage
// error), before any target work.
func TestPiRequiresEndpoint(t *testing.T) {
	h := newTestPiHandler(stubPiSeed{targets: []seed.Target{seed.TargetAnonctl}}, nil, nil, &recordingApplier{})
	var stdout, stderr bytes.Buffer
	code := h.Run(nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (missing --endpoint)", code)
	}
	if !strings.Contains(stderr.String(), "endpoint") {
		t.Errorf("stderr = %q, want it to mention --endpoint", stderr.String())
	}
}

// TestPiExplicitTargetRoutesToApplier: `--target anonctl` selects that substrate
// and routes the plan to the anonctl applier (no detection, no prompt).
func TestPiExplicitTargetRoutesToApplier(t *testing.T) {
	ctl := &recordingApplier{}
	promptCalled := false
	h := piHandler{
		resolveSeed: func(_ context.Context, _ string, _ bool, _, _ io.Writer) (seed.Seed, error) {
			return stubPiSeed{targets: []seed.Target{seed.TargetAnonctl}}, nil
		},
		detector: target.DetectorFunc(func(context.Context) []seed.Target {
			t.Error("detection ran for an explicit --target; it must be bypassed")
			return nil
		}),
		prompt:       func([]seed.Target) ([]seed.Target, error) { promptCalled = true; return nil, nil },
		anonctlApply: ctl.apply,
	}

	var stdout, stderr bytes.Buffer
	code := h.Run([]string{"--endpoint", "127.0.0.1:1234", "--target", "anonctl"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if promptCalled {
		t.Error("prompt ran for an explicit --target; it must be bypassed")
	}
	if len(ctl.got) != 1 {
		t.Errorf("anonctl applier hit %d times, want 1", len(ctl.got))
	}
	if !strings.Contains(stdout.String(), "seeded target \"anonctl\"") {
		t.Errorf("stdout = %q, want an applied-anonctl line", stdout.String())
	}
}

// TestPiUnknownTargetFailsLoud: `--target bogus` fails loudly (non-zero, named).
func TestPiUnknownTargetFailsLoud(t *testing.T) {
	h := newTestPiHandler(stubPiSeed{targets: []seed.Target{seed.TargetAnonctl}}, nil, nil, &recordingApplier{})
	var stdout, stderr bytes.Buffer
	code := h.Run([]string{"--endpoint", "127.0.0.1:1234", "--target", "bogus"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("unknown --target exited 0; it must fail loudly")
	}
	if !strings.Contains(stderr.String(), "bogus") {
		t.Errorf("stderr = %q, want it to name the bad target", stderr.String())
	}
}

// TestPiDefaultDetectsThenAsks: no --target detects the present substrate(s) and
// asks the prompt (never a silent auto-pick), then routes the chosen ones.
func TestPiDefaultDetectsThenAsks(t *testing.T) {
	ctl := &recordingApplier{}
	h := newTestPiHandler(
		stubPiSeed{targets: []seed.Target{seed.TargetAnonctl}},
		[]seed.Target{seed.TargetAnonctl}, // detected present
		[]seed.Target{seed.TargetAnonctl}, // operator's choice
		ctl,
	)
	var stdout, stderr bytes.Buffer
	code := h.Run([]string{"--endpoint", "127.0.0.1:1234"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if len(ctl.got) != 1 {
		t.Errorf("anonctl applier hit %d times, want 1 (detected + chosen)", len(ctl.got))
	}
}

// TestPiMultiTargetFansOut: a seed declaring BOTH substrates, with both detected
// and chosen, fans out to both (anonctl applied, anonbox surfaces the stub's
// not-yet-available error and forces a non-zero exit).
func TestPiMultiTargetFansOut(t *testing.T) {
	ctl := &recordingApplier{}
	h := newTestPiHandler(
		stubPiSeed{targets: []seed.Target{seed.TargetAnonctl, seed.TargetAnonbox}},
		[]seed.Target{seed.TargetAnonctl, seed.TargetAnonbox},
		[]seed.Target{seed.TargetAnonctl, seed.TargetAnonbox},
		ctl,
	)
	var stdout, stderr bytes.Buffer
	code := h.Run([]string{"--endpoint", "127.0.0.1:1234"}, &stdout, &stderr)

	// anonctl applied.
	if len(ctl.got) != 1 {
		t.Errorf("anonctl applier hit %d times, want 1", len(ctl.got))
	}
	if !strings.Contains(stdout.String(), "seeded target \"anonctl\"") {
		t.Errorf("stdout = %q, want an applied-anonctl line", stdout.String())
	}
	// anonbox routed to the real stub -> not-yet-available error, non-zero exit.
	if code == 0 {
		t.Error("multi-target with anonbox exited 0; the anonbox stub error must surface")
	}
	if !strings.Contains(stderr.String(), "anonbox") {
		t.Errorf("stderr = %q, want the anonbox failure named", stderr.String())
	}
}

// TestPiUnsupportedTargetSkippedCleanly: a target the seed does NOT declare is
// skipped cleanly (a clear message, exit 0, the applier untouched) rather than
// erroring. Here the seed supports only anonctl but the operator selects anonbox.
func TestPiUnsupportedTargetSkippedCleanly(t *testing.T) {
	ctl := &recordingApplier{}
	h := piHandler{
		resolveSeed: func(_ context.Context, _ string, _ bool, _, _ io.Writer) (seed.Seed, error) {
			return stubPiSeed{targets: []seed.Target{seed.TargetAnonctl}}, nil // NOT anonbox
		},
		detector: target.DetectorFunc(func(context.Context) []seed.Target {
			return []seed.Target{seed.TargetAnonbox}
		}),
		prompt:       func(p []seed.Target) ([]seed.Target, error) { return p, nil },
		anonctlApply: ctl.apply,
	}

	var stdout, stderr bytes.Buffer
	code := h.Run([]string{"--endpoint", "127.0.0.1:1234"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (an unsupported target is a clean skip, not an error); stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "skipped") {
		t.Errorf("stdout = %q, want a clear skip message", stdout.String())
	}
	if len(ctl.got) != 0 {
		t.Error("anonctl applier was hit though anonctl was not selected")
	}
}

// TestPiNoTargetChosen: the default path where the operator declines every
// substrate (prompt returns none) seeds nothing and exits 0.
func TestPiNoTargetChosen(t *testing.T) {
	ctl := &recordingApplier{}
	h := newTestPiHandler(
		stubPiSeed{targets: []seed.Target{seed.TargetAnonctl}},
		[]seed.Target{seed.TargetAnonctl},
		nil, // operator declines
		ctl,
	)
	var stdout, stderr bytes.Buffer
	code := h.Run([]string{"--endpoint", "127.0.0.1:1234"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if len(ctl.got) != 0 {
		t.Error("applier was hit though no target was chosen")
	}
	if !strings.Contains(stdout.String(), "nothing seeded") {
		t.Errorf("stdout = %q, want a nothing-seeded notice", stdout.String())
	}
}

// TestPiSeedResolutionFailureAborts: a seed-resolution error (e.g. a refused real
// apiKey) aborts before any target work, with a non-zero exit.
func TestPiSeedResolutionFailureAborts(t *testing.T) {
	ctl := &recordingApplier{}
	h := piHandler{
		resolveSeed: func(_ context.Context, _ string, _ bool, _, _ io.Writer) (seed.Seed, error) {
			return nil, errors.New("matched provider apiKey refused")
		},
		detector: target.DetectorFunc(func(context.Context) []seed.Target {
			t.Error("detection ran despite a seed-resolution failure; it must abort first")
			return nil
		}),
		prompt:       func([]seed.Target) ([]seed.Target, error) { return nil, nil },
		anonctlApply: ctl.apply,
	}
	var stdout, stderr bytes.Buffer
	code := h.Run([]string{"--endpoint", "127.0.0.1:1234", "--target", "anonctl"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("a seed-resolution failure exited 0; it must abort loudly")
	}
	if !strings.Contains(stderr.String(), "refused") {
		t.Errorf("stderr = %q, want the resolution error surfaced", stderr.String())
	}
	if len(ctl.got) != 0 {
		t.Error("applier ran despite a seed-resolution failure")
	}
}
