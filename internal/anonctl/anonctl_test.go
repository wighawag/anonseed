package anonctl_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wighawag/anonseed/internal/anonctl"
	"github.com/wighawag/anonseed/internal/seed"
)

// fakeRunner records the chown calls seedhome issues so the home write is
// exercised for real (the file copy is real I/O into a temp base dir) WITHOUT a
// real chown (which needs root). This is the Runner seam the task requires: no
// root, no real chown.
type fakeRunner struct{ calls [][]string }

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return "", "", nil
}

// getentRunner scripts `getent passwd <account>` for the named-account sub-target
// so the passwd lookup runs through the Runner seam with no real account on the
// box. A chown is recorded but returns success.
type getentRunner struct {
	homes map[string]string
	calls [][]string
}

func (r *getentRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if name == "getent" && len(args) == 2 && args[0] == "passwd" {
		home, ok := r.homes[args[1]]
		if !ok {
			return "", "", nil
		}
		return args[1] + ":x:1000:1000::" + home + ":/bin/bash\n", "", nil
	}
	return "", "", nil
}

// samplePlan is a representative plan: two files plus one loopback exception that
// PASSES the allowguard pre-check, so the happy paths exercise both a file write
// and a defaults.json merge.
func samplePlan() seed.SeedPlan {
	return seed.SeedPlan{
		Files: []seed.FileToWrite{
			{Path: ".pi/agent/models.json", Content: `{"providers":{}}`},
			{Path: ".pi/agent/settings.json", Content: `{"defaultModel":"x"}`},
		},
		Exceptions: []seed.Exception{
			{Allow: "127.0.0.1:11434", Reason: "local model endpoint"},
		},
	}
}

// readAllow reads defaults.json under baseDir and returns its "allow" list. It
// fails the test if the file is missing or malformed.
func readAllow(t *testing.T, baseDir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(baseDir, "defaults.json"))
	if err != nil {
		t.Fatalf("read defaults.json: %v", err)
	}
	var rec struct {
		Allow []string `json:"allow"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("defaults.json is not valid JSON: %v (%s)", err, data)
	}
	return rec.Allow
}

// TestApplyDefaultHome covers the FIRST sub-target: the box-wide default-home
// template under a TEMP base dir. The files land under <base>/default-home/ and
// the exception is merged into <base>/defaults.json, all with the real
// /etc/anonctl never referenced.
func TestApplyDefaultHome(t *testing.T) {
	base := t.TempDir()
	r := &fakeRunner{}
	a := anonctl.Applier{BaseDir: base, Runner: r}

	res, err := a.ApplyDefaultHome(context.Background(), samplePlan(), false)
	if err != nil {
		t.Fatalf("ApplyDefaultHome: %v", err)
	}

	// Files landed under the default-home template.
	home := filepath.Join(base, "default-home")
	if got, err := os.ReadFile(filepath.Join(home, ".pi/agent/models.json")); err != nil || string(got) != `{"providers":{}}` {
		t.Errorf("models.json = %q err %v", got, err)
	}
	if res.Home.Copied != 2 {
		t.Errorf("Home.Copied = %d, want 2", res.Home.Copied)
	}
	// The exception was merged into defaults.json.
	if allow := readAllow(t, base); len(allow) != 1 || allow[0] != "127.0.0.1:11434" {
		t.Errorf("allow = %v, want [127.0.0.1:11434]", allow)
	}
	if len(res.AllowAdded) != 1 || res.AllowAdded[0] != "127.0.0.1:11434" {
		t.Errorf("AllowAdded = %v, want [127.0.0.1:11434]", res.AllowAdded)
	}
	// The chown targeted the default anon account, through the Runner seam.
	if len(r.calls) == 0 {
		t.Fatal("expected chown calls through the Runner seam, got none")
	}
	for _, c := range r.calls {
		if c[0] != "chown" || c[1] != "anon:anon" {
			t.Errorf("unexpected Runner call %v, want a chown to anon:anon", c)
		}
	}
}

// TestApplyAccountHome covers the SECOND sub-target: a SPECIFIC named account's
// home, resolved through anoncore's account vocabulary (`work` -> `anon-work`)
// and a scripted passwd lookup over the Runner seam (no real account). The files
// land in that account's home and the exception is merged into defaults.json.
func TestApplyAccountHome(t *testing.T) {
	base := t.TempDir()
	acctHome := filepath.Join(base, "home-anon-work")
	r := &getentRunner{homes: map[string]string{"anon-work": acctHome}}
	a := anonctl.Applier{BaseDir: base, Runner: r}

	res, err := a.ApplyAccountHome(context.Background(), "work", samplePlan(), false)
	if err != nil {
		t.Fatalf("ApplyAccountHome: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(acctHome, ".pi/agent/models.json")); err != nil || string(got) != `{"providers":{}}` {
		t.Errorf("models.json in account home = %q err %v", got, err)
	}
	if res.Home.Copied != 2 {
		t.Errorf("Home.Copied = %d, want 2", res.Home.Copied)
	}
	if allow := readAllow(t, base); len(allow) != 1 || allow[0] != "127.0.0.1:11434" {
		t.Errorf("allow = %v, want [127.0.0.1:11434]", allow)
	}
	// The chown targeted the resolved account, not root.
	sawChown := false
	for _, c := range r.calls {
		if c[0] == "chown" {
			sawChown = true
			if c[1] != "anon-work:anon-work" {
				t.Errorf("chown target = %q, want anon-work:anon-work", c[1])
			}
		}
	}
	if !sawChown {
		t.Error("expected a chown to the named account through the Runner seam")
	}
}

// TestApplyAccountHomeMissing: a named account with no passwd home is a loud error
// (propagated from homewrite), not a silent seed into an empty path.
func TestApplyAccountHomeMissing(t *testing.T) {
	base := t.TempDir()
	r := &getentRunner{homes: map[string]string{}}
	a := anonctl.Applier{BaseDir: base, Runner: r}
	if _, err := a.ApplyAccountHome(context.Background(), "ghost", samplePlan(), false); err == nil {
		t.Fatal("expected an error for an account with no passwd home, got nil")
	}
}

// TestDefaultsCreatedWhenAbsent: with no pre-existing defaults.json, the merge
// CREATES it with exactly the plan's exceptions, in anonctl's `{"allow": [...]}`
// shape.
func TestDefaultsCreatedWhenAbsent(t *testing.T) {
	base := t.TempDir()
	a := anonctl.Applier{BaseDir: base, Runner: &fakeRunner{}}
	if _, err := os.Stat(filepath.Join(base, "defaults.json")); !os.IsNotExist(err) {
		t.Fatalf("defaults.json should not exist yet: %v", err)
	}

	if _, err := a.ApplyDefaultHome(context.Background(), samplePlan(), false); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if allow := readAllow(t, base); len(allow) != 1 || allow[0] != "127.0.0.1:11434" {
		t.Errorf("created allow = %v, want [127.0.0.1:11434]", allow)
	}
}

// TestDefaultsMergePreservesExisting is the no-clobber guarantee: an existing
// defaults.json (with an operator's own exemptions) is PRESERVED, the new
// exception is APPENDED, and the operator's entries survive.
func TestDefaultsMergePreservesExisting(t *testing.T) {
	base := t.TempDir()
	// An operator's hand-written defaults.json already carrying two exemptions.
	existing := `{"allow":["192.168.1.50:8080","10.0.0.5:9000"]}`
	if err := os.WriteFile(filepath.Join(base, "defaults.json"), []byte(existing), 0o644); err != nil {
		t.Fatalf("prep existing defaults.json: %v", err)
	}
	a := anonctl.Applier{BaseDir: base, Runner: &fakeRunner{}}

	res, err := a.ApplyDefaultHome(context.Background(), samplePlan(), false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	allow := readAllow(t, base)
	want := []string{"192.168.1.50:8080", "10.0.0.5:9000", "127.0.0.1:11434"}
	if strings.Join(allow, ",") != strings.Join(want, ",") {
		t.Errorf("merged allow = %v, want %v (existing preserved, new appended)", allow, want)
	}
	// Only the genuinely-new value is reported as added.
	if len(res.AllowAdded) != 1 || res.AllowAdded[0] != "127.0.0.1:11434" {
		t.Errorf("AllowAdded = %v, want [127.0.0.1:11434]", res.AllowAdded)
	}
}

// TestDefaultsMergeNoDuplicates: an exception already present in defaults.json is
// NOT re-appended, so re-running a seed is idempotent (no duplicate entries).
func TestDefaultsMergeNoDuplicates(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "defaults.json"),
		[]byte(`{"allow":["127.0.0.1:11434"]}`), 0o644); err != nil {
		t.Fatalf("prep: %v", err)
	}
	a := anonctl.Applier{BaseDir: base, Runner: &fakeRunner{}}

	res, err := a.ApplyDefaultHome(context.Background(), samplePlan(), false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if allow := readAllow(t, base); len(allow) != 1 || allow[0] != "127.0.0.1:11434" {
		t.Errorf("allow = %v, want a single [127.0.0.1:11434] (no duplicate)", allow)
	}
	if len(res.AllowAdded) != 0 {
		t.Errorf("AllowAdded = %v, want empty (value already present)", res.AllowAdded)
	}
}

// TestApplyRejectsBadExceptionBeforeWriting is the fail-fast guarantee: a plan
// carrying an exception the allowguard pre-check rejects (a public address, no
// port) aborts the WHOLE apply BEFORE any file is written. No home files, no
// defaults.json.
func TestApplyRejectsBadExceptionBeforeWriting(t *testing.T) {
	base := t.TempDir()
	a := anonctl.Applier{BaseDir: base, Runner: &fakeRunner{}}
	bad := seed.SeedPlan{
		Files:      []seed.FileToWrite{{Path: ".pi/agent/models.json", Content: "x"}},
		Exceptions: []seed.Exception{{Allow: "8.8.8.8:443"}}, // public: rejected
	}

	if _, err := a.ApplyDefaultHome(context.Background(), bad, false); err == nil {
		t.Fatal("expected a rejection for a public --allow value, got nil")
	}
	// Nothing was written: neither the home file nor defaults.json.
	if _, err := os.Stat(filepath.Join(base, "default-home", ".pi/agent/models.json")); !os.IsNotExist(err) {
		t.Errorf("a home file was written despite a rejected exception: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "defaults.json")); !os.IsNotExist(err) {
		t.Errorf("defaults.json was written despite a rejected exception: %v", err)
	}
}

// TestPlanWithoutExceptions: a plan with files but no exceptions seeds the home
// and writes NO defaults.json (there is nothing to declare).
func TestPlanWithoutExceptions(t *testing.T) {
	base := t.TempDir()
	a := anonctl.Applier{BaseDir: base, Runner: &fakeRunner{}}
	plan := seed.SeedPlan{Files: []seed.FileToWrite{{Path: "cfg", Content: "x"}}}

	if _, err := a.ApplyDefaultHome(context.Background(), plan, false); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "defaults.json")); !os.IsNotExist(err) {
		t.Errorf("defaults.json created for a plan with no exceptions")
	}
}

// TestDefaultsCorruptIsLoud: a present-but-corrupt defaults.json is a loud error
// (never silently clobbered), so an operator's typo fails visibly rather than
// dropping their existing exemptions. Mirrors anonctl's own read-is-loud stance.
func TestDefaultsCorruptIsLoud(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "defaults.json"), []byte(`{not json`), 0o644); err != nil {
		t.Fatalf("prep: %v", err)
	}
	a := anonctl.Applier{BaseDir: base, Runner: &fakeRunner{}}
	if _, err := a.ApplyDefaultHome(context.Background(), samplePlan(), false); err == nil {
		t.Fatal("expected a loud error for a corrupt defaults.json, got nil")
	}
	// The corrupt file was NOT overwritten.
	if got, _ := os.ReadFile(filepath.Join(base, "defaults.json")); string(got) != `{not json` {
		t.Errorf("corrupt defaults.json was clobbered: %q", got)
	}
}

// TestBaseDirIsolation is the SHARED-WRITE ISOLATION guarantee, mandatory here
// because this applier writes a system path (/etc/anonctl). With the base dir
// pointed at a temp dir, applying a full plan touches ONLY that temp dir: the real
// /etc/anonctl is never referenced, and a sentinel OUTSIDE the base dir is
// unchanged. The Runner sees only chown (no real fs mutation of a system path).
func TestBaseDirIsolation(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "etc-anonctl")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("prep base: %v", err)
	}
	// A sentinel OUTSIDE the base dir, standing in for "the rest of the real
	// filesystem" (including the real /etc/anonctl).
	sentinel := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(sentinel, []byte("UNTOUCHED"), 0o600); err != nil {
		t.Fatalf("prep sentinel: %v", err)
	}

	r := &fakeRunner{}
	a := anonctl.Applier{BaseDir: base, Runner: r}
	if _, err := a.ApplyDefaultHome(context.Background(), samplePlan(), false); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// The sentinel outside the base dir is untouched.
	if got, _ := os.ReadFile(sentinel); string(got) != "UNTOUCHED" {
		t.Errorf("a path OUTSIDE the base dir was modified: sentinel = %q", got)
	}
	// The real /etc/anonctl was never in play: the DefaultBaseDir constant is the
	// system path, and this apply used the temp base instead.
	if anonctl.DefaultBaseDir != "/etc/anonctl" {
		t.Errorf("DefaultBaseDir = %q, want /etc/anonctl", anonctl.DefaultBaseDir)
	}
	// Every Runner call is a chown; no real fs mutation of a system path leaks
	// through the Runner seam.
	for _, c := range r.calls {
		if c[0] != "chown" {
			t.Errorf("Runner saw a non-chown op %v; the applier must only chown", c)
		}
	}
}

// anonctlSeed is a trivial in-test seed declaring the anonctl target, so the
// driver routes to (rather than skips) this applier and we prove a plan reaches
// it end to end.
type anonctlSeed struct{ plan seed.SeedPlan }

func (anonctlSeed) Name() string { return "fake-anonctl" }

func (anonctlSeed) Targets() []seed.Target { return []seed.Target{seed.TargetAnonctl} }

func (s anonctlSeed) Plan(_ context.Context, _ seed.Options, _ seed.Target) (seed.SeedPlan, error) {
	return s.plan, nil
}

// TestDriverRoutesToAnonctlApplier is the end-to-end routing proof: a seed that
// DECLARES the anonctl target is NOT skipped by the driver, and feeding the
// resulting plan to this applier seeds both a home and defaults.json under a temp
// base dir.
func TestDriverRoutesToAnonctlApplier(t *testing.T) {
	base := t.TempDir()
	s := anonctlSeed{plan: samplePlan()}

	res, err := seed.Run(context.Background(), s, seed.Options{}, seed.TargetAnonctl)
	if err != nil {
		t.Fatalf("driver Run: %v", err)
	}
	if res.Skipped {
		t.Fatal("driver SKIPPED a seed that declares anonctl; it must route to the anonctl applier")
	}
	if anonctl.Target != seed.TargetAnonctl {
		t.Errorf("Target = %q, want seed.TargetAnonctl", anonctl.Target)
	}

	a := anonctl.Applier{BaseDir: base, Runner: &fakeRunner{}}
	if _, err := a.ApplyDefaultHome(context.Background(), res.Plan, false); err != nil {
		t.Fatalf("applying the routed plan: %v", err)
	}
	if allow := readAllow(t, base); len(allow) != 1 {
		t.Errorf("routed plan did not merge its exception: allow = %v", allow)
	}
}
