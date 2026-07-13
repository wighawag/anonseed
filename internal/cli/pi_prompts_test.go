package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wighawag/anonseed/internal/piseed"
)

func cands(ids ...string) []piseed.Candidate {
	c := make([]piseed.Candidate, 0, len(ids))
	for _, id := range ids {
		c = append(c, piseed.Candidate{ID: id})
	}
	return c
}

// TestDefaultPickImportsAllAsksDefault: with 2+ candidates the pick imports ALL
// (every endpoint model is wired) and the operator's numbered choice sets the
// default. A 1-based choice maps to the right ID.
func TestDefaultPickImportsAllAsksDefault(t *testing.T) {
	var out bytes.Buffer
	pick, err := defaultPickFrom(strings.NewReader("2\n"), &out, cands("a", "b", "c"))
	if err != nil {
		t.Fatalf("defaultPickFrom: %v", err)
	}
	if len(pick.ImportIDs) != 3 {
		t.Errorf("ImportIDs = %v, want all 3 imported", pick.ImportIDs)
	}
	if pick.DefaultID != "b" {
		t.Errorf("DefaultID = %q, want b (the 2nd choice)", pick.DefaultID)
	}
}

// TestDefaultPickEmptyDefaultsToFirst: an empty answer picks the first candidate
// as the default (still importing all).
func TestDefaultPickEmptyDefaultsToFirst(t *testing.T) {
	var out bytes.Buffer
	pick, err := defaultPickFrom(strings.NewReader("\n"), &out, cands("a", "b"))
	if err != nil {
		t.Fatalf("defaultPickFrom: %v", err)
	}
	if pick.DefaultID != "a" {
		t.Errorf("DefaultID = %q, want a (empty == first)", pick.DefaultID)
	}
}

// TestDefaultPickSingleCandidateNoPrompt: exactly one candidate is trivially the
// default; the prompt is skipped (no output written, no input consumed).
func TestDefaultPickSingleCandidateNoPrompt(t *testing.T) {
	var out bytes.Buffer
	pick, err := defaultPickFrom(strings.NewReader(""), &out, cands("only"))
	if err != nil {
		t.Fatalf("defaultPickFrom: %v", err)
	}
	if pick.DefaultID != "only" || len(pick.ImportIDs) != 1 {
		t.Errorf("single candidate should be default without a prompt, got %+v", pick)
	}
	if out.Len() != 0 {
		t.Errorf("no prompt should be shown for a single candidate, got %q", out.String())
	}
}

// TestDefaultPickZeroCandidates: no candidates yields an empty pick (the model-only
// path), no prompt.
func TestDefaultPickZeroCandidates(t *testing.T) {
	var out bytes.Buffer
	pick, err := defaultPickFrom(strings.NewReader(""), &out, nil)
	if err != nil {
		t.Fatalf("defaultPickFrom: %v", err)
	}
	if len(pick.ImportIDs) != 0 {
		t.Errorf("zero candidates should yield an empty pick, got %+v", pick)
	}
}

// TestDefaultPickInvalidChoiceErrors: an out-of-range or non-numeric choice is a
// loud error (the operator picked something not offered).
func TestDefaultPickInvalidChoiceErrors(t *testing.T) {
	for _, bad := range []string{"9\n", "0\n", "x\n"} {
		var out bytes.Buffer
		if _, err := defaultPickFrom(strings.NewReader(bad), &out, cands("a", "b")); err == nil {
			t.Errorf("choice %q should error", strings.TrimSpace(bad))
		}
	}
}

// TestOverwritePromptYes: an explicit "y" authorises the overwrite and the prompt
// lists the colliding paths so the operator sees what is being clobbered.
func TestOverwritePromptYes(t *testing.T) {
	var out bytes.Buffer
	ok, err := overwritePromptFrom(strings.NewReader("y\n"), &out, []string{".pi/agent/models.json", ".pi/agent/settings.json"})
	if err != nil {
		t.Fatalf("overwritePromptFrom: %v", err)
	}
	if !ok {
		t.Error("answer \"y\" should authorise the overwrite")
	}
	if !strings.Contains(out.String(), ".pi/agent/models.json") || !strings.Contains(out.String(), ".pi/agent/settings.json") {
		t.Errorf("prompt should list the colliding paths, got %q", out.String())
	}
}

// TestOverwritePromptDefaultNo: an empty answer keeps the create-only default (do
// NOT overwrite), so the operator must opt in to clobbering.
func TestOverwritePromptDefaultNo(t *testing.T) {
	for _, in := range []string{"\n", "n\n", "no\n", "anything\n"} {
		var out bytes.Buffer
		ok, err := overwritePromptFrom(strings.NewReader(in), &out, []string{"a"})
		if err != nil {
			t.Fatalf("overwritePromptFrom(%q): %v", in, err)
		}
		if ok {
			t.Errorf("answer %q should NOT authorise the overwrite (create-only default)", strings.TrimSpace(in))
		}
	}
}

// TestOverwritePromptEOFIsNo: no input (EOF / non-interactive) takes the safe
// default: do NOT overwrite, rather than blocking or clobbering.
func TestOverwritePromptEOFIsNo(t *testing.T) {
	var out bytes.Buffer
	ok, err := overwritePromptFrom(strings.NewReader(""), &out, []string{"a"})
	if err != nil {
		t.Fatalf("overwritePromptFrom: %v", err)
	}
	if ok {
		t.Error("EOF should be a NO (safe create-only default)")
	}
}

// present/absent detect seams for the searxng prompt tests.
func detectPresent(path string) func() (piseed.SearxngDetection, error) {
	return func() (piseed.SearxngDetection, error) {
		return piseed.SearxngDetection{Present: true, SocketPath: path}, nil
	}
}
func detectAbsent() (piseed.SearxngDetection, error) {
	return piseed.SearxngDetection{Present: false}, nil
}

// TestSearxngDetectedSkipsPrompt: a detected SearXNG returns immediately, no prompt.
func TestSearxngDetectedSkipsPrompt(t *testing.T) {
	var out bytes.Buffer
	det, err := searxngDetectFrom(strings.NewReader(""), &out, detectPresent("/run/s.sock"), piseed.WebveilChoice{})
	if err != nil {
		t.Fatalf("searxngDetectFrom: %v", err)
	}
	if !det.Present || det.SocketPath != "/run/s.sock" {
		t.Errorf("detected SearXNG should pass through, got %+v", det)
	}
	if out.Len() != 0 {
		t.Errorf("no prompt should show when SearXNG is detected, got %q", out.String())
	}
}

// TestSearxngAbsentProceed: not detected + [p]roceed -> absent (webveil disabled),
// and the prompt pointed at the install guide.
func TestSearxngAbsentProceed(t *testing.T) {
	var out bytes.Buffer
	det, err := searxngDetectFrom(strings.NewReader("p\n"), &out, detectAbsent, piseed.WebveilChoice{})
	if err != nil {
		t.Fatalf("searxngDetectFrom: %v", err)
	}
	if det.Present {
		t.Errorf("proceed should return absent, got %+v", det)
	}
	if !strings.Contains(out.String(), searxngInstallURL) {
		t.Errorf("prompt should point at the install guide %q; got %q", searxngInstallURL, out.String())
	}
	// The guidance must steer to a BARE-METAL install (the seed needs a host uWSGI
	// socket), explicitly NOT Docker.
	if !strings.Contains(out.String(), "NOT Docker") {
		t.Errorf("prompt should warn against a Docker install; got %q", out.String())
	}
}

// TestSearxngAbsentInstallDefault: not detected + [i]nstall-default -> Present with
// an empty socket, so ResolveWebveil wires webveil at the install default.
func TestSearxngAbsentInstallDefault(t *testing.T) {
	var out bytes.Buffer
	det, err := searxngDetectFrom(strings.NewReader("i\n"), &out, detectAbsent, piseed.WebveilChoice{})
	if err != nil {
		t.Fatalf("searxngDetectFrom: %v", err)
	}
	if !det.Present {
		t.Errorf("install-default should report Present (wires the default socket), got %+v", det)
	}
	// The pure decision then wires the install default.
	if w := piseed.ResolveWebveil(det, piseed.WebveilChoice{}); w == nil || w.SocketPath != piseed.DefaultSearxngSocketPath {
		t.Errorf("install-default should wire the default socket via ResolveWebveil, got %+v", w)
	}
}

// TestSearxngRecheckThenFound: not detected, then [r]echeck AFTER the operator
// installed SearXNG (the detect seam flips to present), wires the freshly-detected
// socket. The first detect is absent, the recheck detect is present.
func TestSearxngRecheckThenFound(t *testing.T) {
	calls := 0
	detect := func() (piseed.SearxngDetection, error) {
		calls++
		if calls == 1 {
			return piseed.SearxngDetection{Present: false}, nil // initial: absent
		}
		return piseed.SearxngDetection{Present: true, SocketPath: "/run/new.sock"}, nil // after install
	}
	var out bytes.Buffer
	det, err := searxngDetectFrom(strings.NewReader("r\n"), &out, detect, piseed.WebveilChoice{})
	if err != nil {
		t.Fatalf("searxngDetectFrom: %v", err)
	}
	if !det.Present || det.SocketPath != "/run/new.sock" {
		t.Errorf("recheck after install should wire the newly-detected socket, got %+v", det)
	}
	if !strings.Contains(out.String(), "now detected") {
		t.Errorf("recheck success should confirm detection, got %q", out.String())
	}
}

// TestSearxngEOFProceedsSafely: no input (EOF, e.g. non-interactive) takes the safe
// default: proceed without webveil rather than blocking.
func TestSearxngEOFProceedsSafely(t *testing.T) {
	var out bytes.Buffer
	det, err := searxngDetectFrom(strings.NewReader(""), &out, detectAbsent, piseed.WebveilChoice{})
	if err != nil {
		t.Fatalf("searxngDetectFrom: %v", err)
	}
	if det.Present {
		t.Errorf("EOF should proceed without webveil (absent), got %+v", det)
	}
}

// TestSearxngDisabledSkipsPrompt: an explicit --no-webveil settles the decision, so
// even an absent SearXNG shows no prompt.
func TestSearxngDisabledSkipsPrompt(t *testing.T) {
	var out bytes.Buffer
	_, err := searxngDetectFrom(strings.NewReader(""), &out, detectAbsent, piseed.WebveilChoice{Disabled: true})
	if err != nil {
		t.Fatalf("searxngDetectFrom: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("--no-webveil should skip the not-found prompt, got %q", out.String())
	}
}
