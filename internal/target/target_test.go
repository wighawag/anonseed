package target_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wighawag/anoncore/seedhome"
	"github.com/wighawag/anonseed/internal/anonbox"
	"github.com/wighawag/anonseed/internal/anonctl"
	"github.com/wighawag/anonseed/internal/seed"
	"github.com/wighawag/anonseed/internal/target"
)

// fakeChownRunner satisfies the anonctl Runner seam for the collision-retry tests:
// the file copy is real I/O into a temp base dir, but the chown seedhome issues is
// recorded and returns success (no root needed).
type fakeChownRunner struct{}

func (fakeChownRunner) Run(_ context.Context, _ string, _ ...string) (string, string, error) {
	return "", "", nil
}

// fakeSeed is a trivial in-test seed declaring a chosen set of targets and
// returning a fixed plan. It exercises the fan-out + skip logic without any real
// seed type, so target-axis behaviour is provable in isolation.
type fakeSeed struct {
	name      string
	targets   []seed.Target
	plan      seed.SeedPlan
	planErr   error
	planCalls int
}

func (f *fakeSeed) Name() string { return f.name }

func (f *fakeSeed) Targets() []seed.Target { return f.targets }

func (f *fakeSeed) Plan(_ context.Context, _ seed.Options, _ seed.Target) (seed.SeedPlan, error) {
	f.planCalls++
	return f.plan, f.planErr
}

func samplePlan() seed.SeedPlan {
	return seed.SeedPlan{
		Files:      []seed.FileToWrite{{Path: ".pi/agent/models.json", Content: "{}"}},
		Exceptions: []seed.Exception{{Allow: "127.0.0.1:1234"}},
	}
}

// recordingApplier records the plans it was handed, so a test can assert a fan-out
// routed the right plan to the right substrate.
type recordingApplier struct {
	got []seed.SeedPlan
	err error
}

func (r *recordingApplier) apply(_ context.Context, plan seed.SeedPlan) error {
	r.got = append(r.got, plan)
	return r.err
}

// --- Parse (explicit target) -------------------------------------------------

// TestParseKnownTargets: an explicit --target value naming a known substrate
// parses to that seed.Target.
func TestParseKnownTargets(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want seed.Target
	}{
		{"anonctl", seed.TargetAnonctl},
		{"anonbox", seed.TargetAnonbox},
	} {
		got, err := target.Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q) errored: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("Parse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestParseUnknownFailsLoud: an unknown --target value is a LOUD error naming the
// bad value and the accepted set (never a silent fallback to a default).
func TestParseUnknownFailsLoud(t *testing.T) {
	_, err := target.Parse("anonctrl")
	if err == nil {
		t.Fatal("Parse(unknown) returned nil; an unknown --target must fail loudly")
	}
	msg := err.Error()
	if !strings.Contains(msg, "anonctrl") {
		t.Errorf("error %q should name the offending value", msg)
	}
	if !strings.Contains(msg, "anonctl") || !strings.Contains(msg, "anonbox") {
		t.Errorf("error %q should list the accepted targets", msg)
	}
}

// TestParseEmptyFailsLoud: the empty value is not a valid explicit target (the
// empty case is the DEFAULT detect-then-ask path, via Select, not a Parse input).
func TestParseEmptyFailsLoud(t *testing.T) {
	if _, err := target.Parse(""); err == nil {
		t.Error("Parse(\"\") returned nil; empty is not an explicit target")
	}
}

// --- Select (detect-then-ask default) ---------------------------------------

// TestSelectDetectsThenAsks: with no explicit target, Select DETECTS the present
// substrates and ASKS the operator (the prompt), returning the operator's choice.
// It never silently auto-picks: the prompt is consulted even here.
func TestSelectDetectsThenAsks(t *testing.T) {
	var promptedWith []seed.Target
	det := target.DetectorFunc(func(context.Context) []seed.Target {
		return []seed.Target{seed.TargetAnonctl}
	})
	prompt := func(present []seed.Target) ([]seed.Target, error) {
		promptedWith = present
		return present, nil // operator picks all present
	}

	got, err := target.Select(context.Background(), det, prompt)
	if err != nil {
		t.Fatalf("Select errored: %v", err)
	}
	if !reflect.DeepEqual(promptedWith, []seed.Target{seed.TargetAnonctl}) {
		t.Errorf("prompt was asked with %v, want the detected present set [anonctl]", promptedWith)
	}
	if !reflect.DeepEqual(got, []seed.Target{seed.TargetAnonctl}) {
		t.Errorf("Select = %v, want [anonctl]", got)
	}
}

// TestSelectNeverSilentAutoPickWithSinglePresent: even when EXACTLY ONE substrate
// is present, Select still ASKS (does not auto-select it). Proven by a prompt that
// declines (returns none): Select must honour that, not override it with a silent
// pick.
func TestSelectNeverSilentAutoPickWithSinglePresent(t *testing.T) {
	det := target.DetectorFunc(func(context.Context) []seed.Target {
		return []seed.Target{seed.TargetAnonctl}
	})
	asked := false
	prompt := func(present []seed.Target) ([]seed.Target, error) {
		asked = true
		return nil, nil // operator declines
	}

	got, err := target.Select(context.Background(), det, prompt)
	if err != nil {
		t.Fatalf("Select errored: %v", err)
	}
	if !asked {
		t.Error("Select did NOT ask the prompt with a single present substrate; it must never silent auto-pick")
	}
	if len(got) != 0 {
		t.Errorf("Select = %v, want none (the operator declined)", got)
	}
}

// TestSelectMultiPresentCanChooseAll: when SEVERAL substrates are present, the
// operator can choose all of them, and Select returns them normalised (Known
// order, de-duplicated).
func TestSelectMultiPresentCanChooseAll(t *testing.T) {
	det := target.DetectorFunc(func(context.Context) []seed.Target {
		// Return out of order + a duplicate to prove normalisation.
		return []seed.Target{seed.TargetAnonbox, seed.TargetAnonctl, seed.TargetAnonctl}
	})
	prompt := func(present []seed.Target) ([]seed.Target, error) { return present, nil }

	got, err := target.Select(context.Background(), det, prompt)
	if err != nil {
		t.Fatalf("Select errored: %v", err)
	}
	want := []seed.Target{seed.TargetAnonctl, seed.TargetAnonbox}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Select = %v, want %v (normalised Known order, de-duplicated)", got, want)
	}
}

// TestSelectNoSubstrateDetected: when NOTHING is present, Select returns a clear
// error WITHOUT prompting (there is nothing to offer).
func TestSelectNoSubstrateDetected(t *testing.T) {
	det := target.DetectorFunc(func(context.Context) []seed.Target { return nil })
	prompted := false
	prompt := func([]seed.Target) ([]seed.Target, error) { prompted = true; return nil, nil }

	_, err := target.Select(context.Background(), det, prompt)
	if err == nil {
		t.Fatal("Select with nothing detected returned nil; it must error clearly")
	}
	if prompted {
		t.Error("Select prompted despite no substrate being present; there is nothing to ask about")
	}
}

// TestSelectPromptAbortPropagates: a prompt error aborts selection.
func TestSelectPromptAbortPropagates(t *testing.T) {
	det := target.DetectorFunc(func(context.Context) []seed.Target {
		return []seed.Target{seed.TargetAnonctl}
	})
	sentinel := errors.New("operator ctrl-c")
	prompt := func([]seed.Target) ([]seed.Target, error) { return nil, sentinel }

	_, err := target.Select(context.Background(), det, prompt)
	if !errors.Is(err, sentinel) {
		t.Errorf("Select error = %v, want it to wrap the prompt error", err)
	}
}

// TestSelectRejectsAbsentChoice: a prompt that returns a substrate NOT detected
// present is refused (defence in depth: a prompt cannot conjure an absent
// substrate).
func TestSelectRejectsAbsentChoice(t *testing.T) {
	det := target.DetectorFunc(func(context.Context) []seed.Target {
		return []seed.Target{seed.TargetAnonctl}
	})
	prompt := func([]seed.Target) ([]seed.Target, error) {
		return []seed.Target{seed.TargetAnonbox}, nil // not present
	}

	if _, err := target.Select(context.Background(), det, prompt); err == nil {
		t.Error("Select accepted a choice that was not detected present; it must refuse it")
	}
}

// --- Run (fan-out + skip) ----------------------------------------------------

// TestRunExplicitSingleTargetApplies: a single selected target the seed declares
// is driven through and routed to its applier, yielding an Applied outcome; the
// applier receives the seed's plan.
func TestRunExplicitSingleTargetApplies(t *testing.T) {
	s := &fakeSeed{name: "fake", targets: []seed.Target{seed.TargetAnonctl}, plan: samplePlan()}
	ctl := &recordingApplier{}
	appliers := map[seed.Target]target.Applier{seed.TargetAnonctl: ctl.apply}

	outcomes := target.Run(context.Background(), s, seed.Options{}, []seed.Target{seed.TargetAnonctl}, appliers)

	if len(outcomes) != 1 {
		t.Fatalf("got %d outcomes, want 1", len(outcomes))
	}
	o := outcomes[0]
	if !o.Applied || o.Skipped || o.Err != nil {
		t.Fatalf("outcome = %+v, want Applied", o)
	}
	if len(ctl.got) != 1 || !reflect.DeepEqual(ctl.got[0], samplePlan()) {
		t.Errorf("anonctl applier got %+v, want the seed's plan once", ctl.got)
	}
}

// TestRunMultiTargetFansOut: several applicable+present targets are ALL seeded
// (bounded by the seed's Targets()), each routed to its own applier, in input
// order.
func TestRunMultiTargetFansOut(t *testing.T) {
	s := &fakeSeed{
		name:    "fake",
		targets: []seed.Target{seed.TargetAnonctl, seed.TargetAnonbox},
		plan:    samplePlan(),
	}
	ctl := &recordingApplier{}
	box := &recordingApplier{}
	appliers := map[seed.Target]target.Applier{
		seed.TargetAnonctl: ctl.apply,
		seed.TargetAnonbox: box.apply,
	}

	outcomes := target.Run(context.Background(), s, seed.Options{},
		[]seed.Target{seed.TargetAnonctl, seed.TargetAnonbox}, appliers)

	if len(outcomes) != 2 {
		t.Fatalf("got %d outcomes, want 2", len(outcomes))
	}
	for i, want := range []seed.Target{seed.TargetAnonctl, seed.TargetAnonbox} {
		if outcomes[i].Target != want {
			t.Errorf("outcome[%d].Target = %q, want %q (input order preserved)", i, outcomes[i].Target, want)
		}
		if !outcomes[i].Applied {
			t.Errorf("outcome[%d] = %+v, want Applied", i, outcomes[i])
		}
	}
	if len(ctl.got) != 1 || len(box.got) != 1 {
		t.Errorf("fan-out did not hit each applier once: anonctl=%d anonbox=%d", len(ctl.got), len(box.got))
	}
}

// TestRunSkipsUndeclaredTarget: a selected target the seed does NOT declare in
// Targets() is SKIPPED cleanly (Skipped outcome, no error), and its applier is
// never called (no mis-seed).
func TestRunSkipsUndeclaredTarget(t *testing.T) {
	// The seed supports only anonctl; anonbox is selected but undeclared.
	s := &fakeSeed{name: "fake", targets: []seed.Target{seed.TargetAnonctl}, plan: samplePlan()}
	ctl := &recordingApplier{}
	box := &recordingApplier{}
	appliers := map[seed.Target]target.Applier{
		seed.TargetAnonctl: ctl.apply,
		seed.TargetAnonbox: box.apply,
	}

	outcomes := target.Run(context.Background(), s, seed.Options{},
		[]seed.Target{seed.TargetAnonctl, seed.TargetAnonbox}, appliers)

	if len(outcomes) != 2 {
		t.Fatalf("got %d outcomes, want 2", len(outcomes))
	}
	// anonctl: applied. anonbox: skipped, not errored, applier untouched.
	if !outcomes[0].Applied {
		t.Errorf("anonctl outcome = %+v, want Applied", outcomes[0])
	}
	if !outcomes[1].Skipped || outcomes[1].Err != nil || outcomes[1].Applied {
		t.Errorf("anonbox outcome = %+v, want a clean Skipped (not an error, not applied)", outcomes[1])
	}
	if len(box.got) != 0 {
		t.Error("anonbox applier was called for an undeclared target; a skip must not mis-seed")
	}
}

// TestRunSurfacesApplierError: an applier error (e.g. the anonbox stub's
// not-yet-available) is surfaced on the outcome as an Err, matchable via
// errors.Is, and does NOT abort the other targets' outcomes.
func TestRunSurfacesApplierError(t *testing.T) {
	// The seed declares BOTH, so anonbox is applied (not skipped) and the real
	// anonbox stub applier returns ErrNotYetAvailable.
	s := &fakeSeed{
		name:    "fake",
		targets: []seed.Target{seed.TargetAnonctl, seed.TargetAnonbox},
		plan:    samplePlan(),
	}
	ctl := &recordingApplier{}
	appliers := map[seed.Target]target.Applier{
		seed.TargetAnonctl: ctl.apply,
		seed.TargetAnonbox: func(ctx context.Context, plan seed.SeedPlan) error {
			return anonbox.Apply(ctx, plan)
		},
	}

	outcomes := target.Run(context.Background(), s, seed.Options{},
		[]seed.Target{seed.TargetAnonctl, seed.TargetAnonbox}, appliers)

	if len(outcomes) != 2 {
		t.Fatalf("got %d outcomes, want 2", len(outcomes))
	}
	if !outcomes[0].Applied {
		t.Errorf("anonctl outcome = %+v, want Applied (a later error must not abort earlier targets)", outcomes[0])
	}
	if !errors.Is(outcomes[1].Err, anonbox.ErrNotYetAvailable) {
		t.Errorf("anonbox outcome err = %v, want ErrNotYetAvailable surfaced", outcomes[1].Err)
	}
}

// TestRunPlanErrorSurfaced: when the seed's Plan errors for a target, that target
// yields an Err outcome (the applier is never reached).
func TestRunPlanErrorSurfaced(t *testing.T) {
	sentinel := errors.New("plan boom")
	s := &fakeSeed{name: "fake", targets: []seed.Target{seed.TargetAnonctl}, planErr: sentinel}
	ctl := &recordingApplier{}
	appliers := map[seed.Target]target.Applier{seed.TargetAnonctl: ctl.apply}

	outcomes := target.Run(context.Background(), s, seed.Options{}, []seed.Target{seed.TargetAnonctl}, appliers)

	if len(outcomes) != 1 || !errors.Is(outcomes[0].Err, sentinel) {
		t.Fatalf("outcomes = %+v, want the Plan error surfaced", outcomes)
	}
	if len(ctl.got) != 0 {
		t.Error("applier was called despite a Plan error")
	}
}

// TestRunNoApplierRegistered: a selected+declared target with no applier in the
// map is an error outcome (the caller selected something it cannot deliver).
func TestRunNoApplierRegistered(t *testing.T) {
	s := &fakeSeed{name: "fake", targets: []seed.Target{seed.TargetAnonctl}, plan: samplePlan()}

	outcomes := target.Run(context.Background(), s, seed.Options{}, []seed.Target{seed.TargetAnonctl}, nil)

	if len(outcomes) != 1 || outcomes[0].Err == nil {
		t.Fatalf("outcomes = %+v, want an error for a target with no applier", outcomes)
	}
}

// --- DefaultAppliers + detector wiring --------------------------------------

// TestDefaultAppliersRouteAnonbox: the production applier map routes anonbox to
// the loud not-yet-available stub, so a real fan-out onto anonbox surfaces the
// stub's error (not a silent success).
func TestDefaultAppliersRouteAnonbox(t *testing.T) {
	appliers := target.DefaultAppliers(func(context.Context, seed.SeedPlan) error { return nil })
	err := appliers[seed.TargetAnonbox](context.Background(), samplePlan())
	if !errors.Is(err, anonbox.ErrNotYetAvailable) {
		t.Errorf("anonbox applier = %v, want ErrNotYetAvailable", err)
	}
}

// TestEnvDetectorSniffsAnonctlBaseDir: the production detector reports anonctl
// present iff its base dir exists (driven via the base-dir override so no test
// reads the real /etc/anonctl). anonbox is never reported present (it does not
// exist yet).
func TestEnvDetectorSniffsAnonctlBaseDir(t *testing.T) {
	// Absent: a base dir that does not exist -> anonctl not present.
	absent := target.EnvDetector{AnonctlBaseDir: filepath.Join(t.TempDir(), "nope")}
	if got := absent.Detect(context.Background()); len(got) != 0 {
		t.Errorf("Detect with a missing base dir = %v, want none present", got)
	}

	// Present: an existing base dir -> anonctl present, anonbox still absent.
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	present := target.EnvDetector{AnonctlBaseDir: dir}
	got := present.Detect(context.Background())
	if !reflect.DeepEqual(got, []seed.Target{seed.TargetAnonctl}) {
		t.Errorf("Detect with an existing base dir = %v, want [anonctl] (anonbox never present yet)", got)
	}
}

// --- AnonctlDefaultHomeApplier overwrite policy ------------------------------
//
// These exercise the create-only-first / overwrite-on-policy behaviour against a
// REAL anonctl applier pointed at a temp base dir, so the seedhome collision is a
// genuine one (not a mocked error). The first apply seeds the home; the second
// apply is the collision, and the policy decides whether it overwrites.

// applierIntoTempHome returns an AnonctlDefaultHomeApplier over a fresh temp base
// dir with the given policy, plus the base dir so a test can assert file contents.
func applierIntoTempHome(t *testing.T, policy target.OverwritePolicy) (target.Applier, string) {
	t.Helper()
	base := t.TempDir()
	a := anonctl.Applier{BaseDir: base, Runner: fakeChownRunner{}}
	return target.AnonctlDefaultHomeApplier(a, policy), base
}

// TestAnonctlApplierFirstSeedCreatesFiles: with nothing pre-existing, the first
// apply lands the plan's files (the create-only happy path, no policy consulted).
func TestAnonctlApplierFirstSeedCreatesFiles(t *testing.T) {
	policyCalled := false
	policy := func([]string) (bool, error) { policyCalled = true; return true, nil }
	apply, base := applierIntoTempHome(t, policy)

	if err := apply(context.Background(), samplePlan()); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if policyCalled {
		t.Error("policy consulted on a first seed (no collision); it must only fire on a collision")
	}
	if _, err := os.Stat(filepath.Join(base, "default-home", ".pi", "agent", "models.json")); err != nil {
		t.Errorf("first seed should have written the file: %v", err)
	}
}

// TestAnonctlApplierCollisionDeclinedSurfacesError: re-applying onto an existing
// seed with a DECLINING policy (NeverOverwrite) leaves the collision error
// standing (nothing overwritten) rather than clobbering.
func TestAnonctlApplierCollisionDeclinedSurfacesError(t *testing.T) {
	apply, base := applierIntoTempHome(t, target.NeverOverwrite)
	if err := apply(context.Background(), samplePlan()); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	// Second apply with DIFFERENT content: the decline must keep the ORIGINAL bytes.
	changed := seed.SeedPlan{Files: []seed.FileToWrite{{Path: ".pi/agent/models.json", Content: "CHANGED"}}}
	err := apply(context.Background(), changed)
	var collision *seedhome.ErrCollision
	if !errors.As(err, &collision) {
		t.Fatalf("declined overwrite should surface the collision, got %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(base, "default-home", ".pi", "agent", "models.json"))
	if string(got) != "{}" {
		t.Errorf("declined overwrite must not change the file; got %q, want the original {}", got)
	}
}

// TestAnonctlApplierCollisionAuthorisedOverwrites: re-applying with an AUTHORISING
// policy (AlwaysOverwrite, the --overwrite flag) clobbers the existing file with
// the new content and reports the colliding paths to the policy.
func TestAnonctlApplierCollisionAuthorisedOverwrites(t *testing.T) {
	var gotPaths []string
	policy := func(paths []string) (bool, error) { gotPaths = paths; return true, nil }
	apply, base := applierIntoTempHome(t, policy)
	if err := apply(context.Background(), samplePlan()); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	changed := seed.SeedPlan{Files: []seed.FileToWrite{{Path: ".pi/agent/models.json", Content: "CHANGED"}}}
	if err := apply(context.Background(), changed); err != nil {
		t.Fatalf("authorised overwrite should succeed, got %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(base, "default-home", ".pi", "agent", "models.json"))
	if string(got) != "CHANGED" {
		t.Errorf("authorised overwrite should have clobbered the file; got %q", got)
	}
	found := false
	for _, p := range gotPaths {
		if strings.Contains(p, "models.json") {
			found = true
		}
	}
	if !found {
		t.Errorf("policy should receive the colliding paths, got %v", gotPaths)
	}
}

// TestAnonctlApplierPolicyErrorAborts: a policy that ERRORS aborts the apply with
// that error (no overwrite attempted).
func TestAnonctlApplierPolicyErrorAborts(t *testing.T) {
	boom := errors.New("policy boom")
	apply, _ := applierIntoTempHome(t, func([]string) (bool, error) { return false, boom })
	if err := apply(context.Background(), samplePlan()); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	changed := seed.SeedPlan{Files: []seed.FileToWrite{{Path: ".pi/agent/models.json", Content: "X"}}}
	if err := apply(context.Background(), changed); !errors.Is(err, boom) {
		t.Errorf("policy error should abort with that error, got %v", err)
	}
}

// TestAnonctlApplierNilPolicyIsCreateOnly: a nil policy means create-only with no
// fallback (a collision surfaces, never overwritten).
func TestAnonctlApplierNilPolicyIsCreateOnly(t *testing.T) {
	apply, _ := applierIntoTempHome(t, nil)
	if err := apply(context.Background(), samplePlan()); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	err := apply(context.Background(), samplePlan())
	var collision *seedhome.ErrCollision
	if !errors.As(err, &collision) {
		t.Errorf("nil policy should stay create-only (surface the collision), got %v", err)
	}
}
