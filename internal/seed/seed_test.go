package seed

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// fakeSeed is a trivial in-test seed that returns a fixed SeedPlan and supports
// exactly one target. It exercises the whole interface seam without touching
// disk or network, so the seam is provable without any real substrate.
type fakeSeed struct {
	name    string
	targets []Target
	plan    SeedPlan
	// planCalls counts Plan invocations, so a test can assert Plan was (or was
	// not) called.
	planCalls int
}

func (f *fakeSeed) Name() string { return f.name }

func (f *fakeSeed) Targets() []Target { return f.targets }

func (f *fakeSeed) Plan(_ context.Context, _ Options, target Target) (SeedPlan, error) {
	f.planCalls++
	return f.plan, nil
}

// samplePlan is a representative plan: two files and a single exception, used to
// exercise both the driver and the JSON round-trip.
func samplePlan() SeedPlan {
	return SeedPlan{
		Files: []FileToWrite{
			{Path: ".pi/agent/models.json", Content: `{"providers":{}}`},
			{Path: ".pi/agent/settings.json", Content: `{"defaultProvider":"local"}`},
		},
		Exceptions: []Exception{
			{Allow: "127.0.0.1:1234", Reason: "local model endpoint"},
		},
	}
}

// TestDriverRunsDeclaredTarget: when the seed declares the requested target, the
// driver calls Plan and returns the plan, not skipped.
func TestDriverRunsDeclaredTarget(t *testing.T) {
	f := &fakeSeed{name: "fake", targets: []Target{TargetAnonctl}, plan: samplePlan()}

	res, err := Run(context.Background(), f, Options{}, TargetAnonctl)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.Skipped {
		t.Fatalf("Run skipped a declared target; want it applied")
	}
	if f.planCalls != 1 {
		t.Errorf("Plan called %d times, want 1", f.planCalls)
	}
	if !reflect.DeepEqual(res.Plan, samplePlan()) {
		t.Errorf("Run returned plan %+v, want %+v", res.Plan, samplePlan())
	}
	if res.Seed != "fake" || res.Target != TargetAnonctl {
		t.Errorf("Result identity = (%q,%q), want (fake,anonctl)", res.Seed, res.Target)
	}
}

// TestDriverSkipsUndeclaredTarget: the driver skips a seed for a target the seed
// does not declare, with a clear non-fatal outcome (no error, no Plan call, no
// mis-seed).
func TestDriverSkipsUndeclaredTarget(t *testing.T) {
	f := &fakeSeed{name: "fake", targets: []Target{TargetAnonctl}, plan: samplePlan()}

	res, err := Run(context.Background(), f, Options{}, TargetAnonbox)
	if err != nil {
		t.Fatalf("skip should not be an error, got: %v", err)
	}
	if !res.Skipped {
		t.Fatalf("Run did not skip an undeclared target")
	}
	if f.planCalls != 0 {
		t.Errorf("Plan was called %d times for a skipped target, want 0", f.planCalls)
	}
	if !reflect.DeepEqual(res.Plan, SeedPlan{}) {
		t.Errorf("skipped Result carried a non-zero plan: %+v", res.Plan)
	}
	if res.Seed != "fake" || res.Target != TargetAnonbox {
		t.Errorf("Result identity = (%q,%q), want (fake,anonbox)", res.Seed, res.Target)
	}
}

// TestPlanIsPure: Plan is deterministic — repeated calls with the same inputs
// yield an equal plan, and calling it does not mutate its inputs. (The interface
// gives Plan no filesystem/network handle, so purity is structural; this asserts
// the determinism half a caller relies on.)
func TestPlanIsPure(t *testing.T) {
	f := &fakeSeed{name: "fake", targets: []Target{TargetAnonctl}, plan: samplePlan()}
	opts := Options{Endpoint: "127.0.0.1:1234"}

	p1, err := f.Plan(context.Background(), opts, TargetAnonctl)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	p2, err := f.Plan(context.Background(), opts, TargetAnonctl)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	if !reflect.DeepEqual(p1, p2) {
		t.Errorf("Plan not deterministic: %+v != %+v", p1, p2)
	}
	if opts.Endpoint != "127.0.0.1:1234" {
		t.Errorf("Plan mutated its Options input: %+v", opts)
	}
}

// TestSeedPlanJSONRoundTrip: SeedPlan marshals to and from JSON losslessly,
// including the multi-file and single-exception case. This is the property the
// reserved PATH-plugin relies on (it emits a SeedPlan on stdout).
func TestSeedPlanJSONRoundTrip(t *testing.T) {
	orig := samplePlan()

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var back SeedPlan
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !reflect.DeepEqual(orig, back) {
		t.Errorf("JSON round-trip lost data:\n orig = %+v\n back = %+v", orig, back)
	}
}

// TestSeedPlanJSONZeroExceptions: a plan with ZERO exceptions (a socket-wired
// service) round-trips too, and its "exceptions" is an explicit empty list, not
// dropped — so a consumer can distinguish "no holes needed" cleanly.
func TestSeedPlanJSONZeroExceptions(t *testing.T) {
	orig := SeedPlan{
		Files:      []FileToWrite{{Path: ".pi/agent/models.json", Content: "{}"}},
		Exceptions: []Exception{},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var back SeedPlan
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(back.Exceptions) != 0 {
		t.Errorf("zero-exception plan did not round-trip: %+v", back)
	}
}

// TestSeedPlanJSONMultipleExceptions: the Exceptions list holds more than one
// entry and round-trips, proving it is genuinely a list (not a single value).
func TestSeedPlanJSONMultipleExceptions(t *testing.T) {
	orig := SeedPlan{
		Exceptions: []Exception{
			{Allow: "127.0.0.1:1234"},
			{Allow: "127.0.0.1:8080", Reason: "second local service"},
		},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var back SeedPlan
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if len(back.Exceptions) != 2 {
		t.Fatalf("multi-exception plan lost entries: %+v", back.Exceptions)
	}
	if !reflect.DeepEqual(orig, back) {
		t.Errorf("multi-exception round-trip mismatch:\n orig = %+v\n back = %+v", orig, back)
	}
}

// TestSeedPlanIsConfigSeedingOnly asserts the config-seeding-ONLY invariant at
// the TYPE level (spec story 2, task acceptance): a SeedPlan can carry ONLY
// files-to-write and exceptions-to-declare. There must be NO field on SeedPlan,
// FileToWrite, or Exception whose name suggests running/launching a service or
// provisioning an account. This is a structural guard so a later seed cannot
// drift into launching by adding such a field without this test flagging it.
func TestSeedPlanIsConfigSeedingOnly(t *testing.T) {
	// The full set of fields the declarative surface is ALLOWED to expose.
	allowed := map[string]map[string]bool{
		"SeedPlan":    {"Files": true, "Exceptions": true},
		"FileToWrite": {"Path": true, "Content": true},
		"Exception":   {"Allow": true, "Reason": true},
	}

	// Any field name containing one of these tokens would smuggle a lifecycle /
	// provisioning affordance into the declarative surface.
	forbiddenTokens := []string{
		"run", "exec", "launch", "start", "stop", "restart",
		"service", "daemon", "command", "cmd", "script",
		"provision", "account", "install", "spawn", "process",
	}

	types := []struct {
		name string
		typ  reflect.Type
	}{
		{"SeedPlan", reflect.TypeOf(SeedPlan{})},
		{"FileToWrite", reflect.TypeOf(FileToWrite{})},
		{"Exception", reflect.TypeOf(Exception{})},
	}

	for _, ty := range types {
		for i := 0; i < ty.typ.NumField(); i++ {
			field := ty.typ.Field(i)

			if !allowed[ty.name][field.Name] {
				t.Errorf("%s has unexpected field %q: the declarative surface must expose ONLY %v (config-seeding-only invariant)",
					ty.name, field.Name, keys(allowed[ty.name]))
			}

			lower := strings.ToLower(field.Name)
			for _, tok := range forbiddenTokens {
				if strings.Contains(lower, tok) {
					t.Errorf("%s.%s contains forbidden token %q: SeedPlan must not express launching/provisioning (config-seeding-only invariant)",
						ty.name, field.Name, tok)
				}
			}
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestSeedInterfaceSatisfied is a compile-time-ish check that the fake seed (and
// thus the shape used elsewhere) satisfies the Seed interface.
func TestSeedInterfaceSatisfied(t *testing.T) {
	var _ Seed = (*fakeSeed)(nil)
}
