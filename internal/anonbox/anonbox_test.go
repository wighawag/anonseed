package anonbox

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wighawag/anonseed/internal/seed"
)

// anonboxSeed is a trivial in-test seed that DECLARES the anonbox target, so the
// driver routes to anonbox (rather than skipping it) and we can prove the plan
// reaches this applier. It supports only anonbox, to keep the routing assertion
// unambiguous.
type anonboxSeed struct {
	plan seed.SeedPlan
}

func (anonboxSeed) Name() string { return "fake-anonbox" }

func (anonboxSeed) Targets() []seed.Target { return []seed.Target{seed.TargetAnonbox} }

func (s anonboxSeed) Plan(_ context.Context, _ seed.Options, _ seed.Target) (seed.SeedPlan, error) {
	return s.plan, nil
}

// samplePlan is a representative plan (a file + an exception) so Apply is
// exercised with a non-empty plan, proving the not-yet-available outcome does not
// depend on the plan being empty.
func samplePlan() seed.SeedPlan {
	return seed.SeedPlan{
		Files: []seed.FileToWrite{
			{Path: ".pi/agent/models.json", Content: `{"providers":{}}`},
		},
		Exceptions: []seed.Exception{
			{Allow: "127.0.0.1:1234", Reason: "local model endpoint"},
		},
	}
}

// TestApplyYieldsNotYetAvailable is the core stub guarantee: applying to the
// anonbox substrate returns a LOUD not-yet-available error (matchable via
// errors.Is), NOT a silent success. The message names the substrate so the
// operator sees a clear notice.
func TestApplyYieldsNotYetAvailable(t *testing.T) {
	err := Apply(context.Background(), samplePlan())
	if err == nil {
		t.Fatal("Apply returned nil (a silent success); the anonbox stub must fail LOUD")
	}
	if !errors.Is(err, ErrNotYetAvailable) {
		t.Errorf("Apply error = %v, want it to wrap ErrNotYetAvailable", err)
	}
	if !strings.Contains(err.Error(), "anonbox") {
		t.Errorf("Apply error %q should name the anonbox substrate", err.Error())
	}
	if !strings.Contains(err.Error(), "not yet available") {
		t.Errorf("Apply error %q should say it is not yet available", err.Error())
	}
}

// TestApplyEmptyPlanStillNotYetAvailable: even an EMPTY plan (no files, no
// exceptions) yields the not-yet-available outcome, not a vacuous success. A
// caller must never read "nothing to do" as "anonbox delivered it".
func TestApplyEmptyPlanStillNotYetAvailable(t *testing.T) {
	if err := Apply(context.Background(), seed.SeedPlan{}); !errors.Is(err, ErrNotYetAvailable) {
		t.Errorf("Apply(empty plan) = %v, want ErrNotYetAvailable", err)
	}
}

// TestTargetIsAnonbox pins the applier's target to seed.TargetAnonbox, so the
// target axis wiring names the same substrate the driver routes on.
func TestTargetIsAnonbox(t *testing.T) {
	if Target != seed.TargetAnonbox {
		t.Errorf("Target = %q, want seed.TargetAnonbox (%q)", Target, seed.TargetAnonbox)
	}
	if Target != "anonbox" {
		t.Errorf("Target = %q, want the literal substrate name %q", Target, "anonbox")
	}
}

// TestDriverRoutesToAnonboxApplier is the end-to-end routing proof: a seed that
// DECLARES the anonbox target is NOT skipped by the driver (seed.Run resolves +
// routes it), and feeding the resulting plan to this applier yields the
// not-yet-available outcome. This is the "target resolves + routes, apply yields
// not-yet-available" acceptance in one flow.
func TestDriverRoutesToAnonboxApplier(t *testing.T) {
	s := anonboxSeed{plan: samplePlan()}

	res, err := seed.Run(context.Background(), s, seed.Options{}, seed.TargetAnonbox)
	if err != nil {
		t.Fatalf("driver Run returned error: %v", err)
	}
	if res.Skipped {
		t.Fatal("driver SKIPPED a seed that declares anonbox; it must route (resolve) to the anonbox applier")
	}
	if res.Target != seed.TargetAnonbox {
		t.Errorf("routed target = %q, want anonbox", res.Target)
	}

	// The routed plan reaches the applier -> loud not-yet-available.
	if err := Apply(context.Background(), res.Plan); !errors.Is(err, ErrNotYetAvailable) {
		t.Errorf("applying the routed plan = %v, want ErrNotYetAvailable", err)
	}
}

// TestSeedCanDeclareAnonboxTarget proves a seed CAN list anonbox in its
// Targets() and be routed there (the "seeds declare it in Targets()" half of the
// acceptance): a seed declaring anonbox is not skipped for it. The pi Seed itself
// (task pi-seed-model-config) does not exist yet; this asserts the MECHANISM a
// real seed uses to declare the anonbox target is live and reaches this applier.
func TestSeedCanDeclareAnonboxTarget(t *testing.T) {
	s := anonboxSeed{}

	declared := false
	for _, tgt := range s.Targets() {
		if tgt == seed.TargetAnonbox {
			declared = true
		}
	}
	if !declared {
		t.Fatal("test seed does not declare anonbox in Targets(); the declaration mechanism is broken")
	}

	res, err := seed.Run(context.Background(), s, seed.Options{}, seed.TargetAnonbox)
	if err != nil || res.Skipped {
		t.Fatalf("a seed declaring anonbox was not routed to it: skipped=%v err=%v", res.Skipped, err)
	}
}
