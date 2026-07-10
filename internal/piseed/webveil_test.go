package piseed

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wighawag/anonseed/internal/apikeyguard"
	"github.com/wighawag/anonseed/internal/seed"
)

// gotWebveilConfig is the shape webveil's resolveConfig consumes, so the
// assertions read the ACTUAL JSON webveil would load (not the internal struct).
type gotWebveilConfig struct {
	Backend string `json:"backend"`
	BaseURL string `json:"baseUrl"`
	Egress  struct {
		Mode string `json:"mode"`
	} `json:"egress"`
	FetchEgress struct {
		Mode string `json:"mode"`
	} `json:"fetchEgress"`
}

func mustWebveilConfig(t *testing.T, socketPath string) gotWebveilConfig {
	t.Helper()
	b, err := GenerateWebveilConfig(socketPath)
	if err != nil {
		t.Fatalf("GenerateWebveilConfig: %v", err)
	}
	var c gotWebveilConfig
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("emitted webveil config.json did not parse: %v\n%s", err, b)
	}
	return c
}

// TestWebveilConfigShape asserts the anonctl webveil contract: backend searxng, a
// unix: baseUrl at the given socket, and BOTH egress hops direct (required for a
// socket backend under anonctl).
func TestWebveilConfigShape(t *testing.T) {
	c := mustWebveilConfig(t, "/run/searxng/acct.sock")
	if c.Backend != "searxng" {
		t.Errorf("backend = %q, want searxng", c.Backend)
	}
	if c.BaseURL != "unix:/run/searxng/acct.sock" {
		t.Errorf("baseUrl = %q, want unix:/run/searxng/acct.sock", c.BaseURL)
	}
	if c.Egress.Mode != "direct" {
		t.Errorf("egress.mode = %q, want direct (REQUIRED for a socket backend)", c.Egress.Mode)
	}
	if c.FetchEgress.Mode != "direct" {
		t.Errorf("fetchEgress.mode = %q, want direct (anonctl forces egress at the kernel)", c.FetchEgress.Mode)
	}
}

// TestWebveilConfigUnixBaseUrlPrefix: the baseUrl is always the `unix:` socket
// form (never a TCP http:// URL), so webveil binds a socket-scoped Agent and no
// --allow hole is implied.
func TestWebveilConfigUnixBaseUrlPrefix(t *testing.T) {
	c := mustWebveilConfig(t, DefaultSearxngSocketPath)
	if !strings.HasPrefix(c.BaseURL, "unix:") {
		t.Errorf("baseUrl %q must be the unix: socket form", c.BaseURL)
	}
	if strings.Contains(c.BaseURL, "http://") || strings.Contains(c.BaseURL, "://") {
		t.Errorf("baseUrl %q must NOT be a TCP URL (a socket has no IP/port)", c.BaseURL)
	}
}

// TestWebveilConfigEmptySocketUsesInstallDefault: a blank socket path yields the
// install-default socket, never a malformed `unix:` baseUrl.
func TestWebveilConfigEmptySocketUsesInstallDefault(t *testing.T) {
	c := mustWebveilConfig(t, "")
	if c.BaseURL != "unix:"+DefaultSearxngSocketPath {
		t.Errorf("empty socket -> baseUrl %q, want the install default unix:%s", c.BaseURL, DefaultSearxngSocketPath)
	}
}

// TestWebveilConfigJSONStyle: the emitted config is tab-indented with a trailing
// newline (matching the repo's on-disk style / the model files).
func TestWebveilConfigJSONStyle(t *testing.T) {
	b, err := GenerateWebveilConfig("/s")
	if err != nil {
		t.Fatalf("GenerateWebveilConfig: %v", err)
	}
	if !strings.HasSuffix(string(b), "}\n") {
		t.Errorf("config should end with a trailing newline:\n%q", b)
	}
	if !strings.Contains(string(b), "\n\t\"backend\"") {
		t.Errorf("config should be tab-indented:\n%s", b)
	}
}

// --- the decision tree (ResolveWebveil) ---

// TestResolveWebveilDetectedWiresSocket: a detected SearXNG wires webveil at the
// detected socket path (default-on).
func TestResolveWebveilDetectedWiresSocket(t *testing.T) {
	det := SearxngDetection{Present: true, SocketPath: "/run/searxng/x.sock"}
	w := ResolveWebveil(det, WebveilChoice{})
	if w == nil {
		t.Fatal("a detected SearXNG should wire webveil (default-on)")
	}
	if w.SocketPath != "/run/searxng/x.sock" {
		t.Errorf("socket = %q, want the detected path", w.SocketPath)
	}
}

// TestResolveWebveilDetectedNoPathFallsBackToDefault: a detected install whose
// socket path could not be read wires at the install default (still on).
func TestResolveWebveilDetectedNoPathFallsBackToDefault(t *testing.T) {
	w := ResolveWebveil(SearxngDetection{Present: true, SocketPath: ""}, WebveilChoice{})
	if w == nil || w.SocketPath != DefaultSearxngSocketPath {
		t.Errorf("detected-without-path should wire the install default, got %+v", w)
	}
}

// TestResolveWebveilDisabledIsModelOnly: the disable flag turns webveil OFF even
// when SearXNG is detected (the one way to opt out of the default).
func TestResolveWebveilDisabledIsModelOnly(t *testing.T) {
	det := SearxngDetection{Present: true, SocketPath: "/run/x.sock"}
	if w := ResolveWebveil(det, WebveilChoice{Disabled: true}); w != nil {
		t.Errorf("Disabled should yield the model-only fallback (nil), got %+v", w)
	}
}

// TestResolveWebveilOverrideWinsOverDetection: an operator socket override wires
// at that socket, beating detection.
func TestResolveWebveilOverrideWinsOverDetection(t *testing.T) {
	det := SearxngDetection{Present: true, SocketPath: "/run/detected.sock"}
	w := ResolveWebveil(det, WebveilChoice{SocketPathOverride: " /run/chosen.sock "})
	if w == nil || w.SocketPath != "/run/chosen.sock" {
		t.Errorf("override should win (trimmed), got %+v", w)
	}
}

// TestResolveWebveilAbsentDeclinedIsModelOnly: no SearXNG and NOT accepting the
// install default is the EXPLICIT model-only fallback (nil), never silent.
func TestResolveWebveilAbsentDeclinedIsModelOnly(t *testing.T) {
	if w := ResolveWebveil(SearxngDetection{Present: false}, WebveilChoice{}); w != nil {
		t.Errorf("absent + declined should be the model-only fallback (nil), got %+v", w)
	}
}

// TestResolveWebveilAbsentAcceptedInstallDefault: no SearXNG but the operator
// accepts the install default wires webveil at the default socket (they will
// provide SearXNG there) — an explicit choice, not a silent default.
func TestResolveWebveilAbsentAcceptedInstallDefault(t *testing.T) {
	w := ResolveWebveil(SearxngDetection{Present: false}, WebveilChoice{AcceptInstallDefaultWhenAbsent: true})
	if w == nil || w.SocketPath != DefaultSearxngSocketPath {
		t.Errorf("absent + accepted should wire the install default, got %+v", w)
	}
}

// --- Plan integration: webveil file present/absent + no exception ---

// TestPlanWebveilEnabledEmitsConfigNoException: with webveil enabled, Plan adds a
// webveil config.json at the XDG path (a DIFFERENT subtree than .pi/) and adds NO
// --allow exception for the socket (the model endpoint stays the only exception).
func TestPlanWebveilEnabledEmitsConfigNoException(t *testing.T) {
	opts := Options{
		Models:         []Model{{ID: "m"}},
		DefaultModelID: "m",
		APIKey:         apikeyguard.PlaceholderAPIKey,
		Webveil:        &WebveilOptions{SocketPath: "/run/searxng/acct.sock"},
	}
	opts.Endpoint = "127.0.0.1:11434"

	plan, err := opts.Plan(seed.TargetAnonctl)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	files := map[string]string{}
	for _, f := range plan.Files {
		files[f.Path] = f.Content
	}
	cfg, ok := files[WebveilConfigPath]
	if !ok {
		t.Fatalf("webveil config not emitted at %s; got files %v", WebveilConfigPath, keysOf(files))
	}
	// The config path is the XDG subtree, NOT under .pi/.
	if WebveilConfigPath != ".config/webveil/config.json" {
		t.Errorf("webveil config path = %q, want .config/webveil/config.json", WebveilConfigPath)
	}
	if strings.HasPrefix(WebveilConfigPath, ".pi/") {
		t.Errorf("webveil config must NOT be under .pi/: %q", WebveilConfigPath)
	}
	// The config is the socket/direct/direct shape.
	for _, want := range []string{`"backend": "searxng"`, `"baseUrl": "unix:/run/searxng/acct.sock"`, `"egress"`, `"fetchEgress"`} {
		if !strings.Contains(cfg, want) {
			t.Errorf("webveil config missing %q:\n%s", want, cfg)
		}
	}
	// The ONLY exception is the model endpoint; webveil adds NONE (socket).
	if len(plan.Exceptions) != 1 || plan.Exceptions[0].Allow != "127.0.0.1:11434" {
		t.Errorf("exceptions = %+v, want only the model endpoint (webveil socket needs no --allow)", plan.Exceptions)
	}
}

// TestPlanWebveilDisabledOmitsConfig: with webveil nil (the model-only fallback),
// Plan emits ONLY the two model files and the model exception — no webveil config.
func TestPlanWebveilDisabledOmitsConfig(t *testing.T) {
	opts := Options{
		Models:         []Model{{ID: "m"}},
		DefaultModelID: "m",
		APIKey:         apikeyguard.PlaceholderAPIKey,
		Webveil:        nil,
	}
	opts.Endpoint = "127.0.0.1:11434"

	plan, err := opts.Plan(seed.TargetAnonctl)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Files) != 2 {
		t.Fatalf("model-only fallback should emit 2 files, got %d: %v", len(plan.Files), plan.Files)
	}
	for _, f := range plan.Files {
		if f.Path == WebveilConfigPath {
			t.Errorf("webveil config emitted though webveil is disabled: %s", f.Path)
		}
	}
}

// TestPlanWebveilDefaultSocket: an enabled webveil with an empty socket path still
// emits a valid config at the install default (a bare enable never malforms).
func TestPlanWebveilDefaultSocket(t *testing.T) {
	opts := Options{
		Models:         []Model{{ID: "m"}},
		DefaultModelID: "m",
		APIKey:         apikeyguard.PlaceholderAPIKey,
		Webveil:        &WebveilOptions{},
	}
	opts.Endpoint = "127.0.0.1:11434"
	plan, err := opts.Plan(seed.TargetAnonctl)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	var cfg string
	for _, f := range plan.Files {
		if f.Path == WebveilConfigPath {
			cfg = f.Content
		}
	}
	if !strings.Contains(cfg, "unix:"+DefaultSearxngSocketPath) {
		t.Errorf("empty socket should wire the install default:\n%s", cfg)
	}
}
