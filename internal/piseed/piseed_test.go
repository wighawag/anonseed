package piseed

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wighawag/anonseed/internal/apikeyguard"
	"github.com/wighawag/anonseed/internal/seed"
)

// --- pure synthesis: models.json shape ---

// parseModelsJSON parses the emitted models.json into a shape the assertions can
// inspect, so the tests read the actual JSON pi would load (not internal structs).
type gotModelsFile struct {
	Providers map[string]struct {
		API     string                       `json:"api"`
		APIKey  string                       `json:"apiKey"`
		BaseURL string                       `json:"baseUrl"`
		Models  []map[string]json.RawMessage `json:"models"`
	} `json:"providers"`
}

func mustModelsJSON(t *testing.T, endpoint string, models []Model, apiKey string) gotModelsFile {
	t.Helper()
	b, err := GenerateModelsJSON(endpoint, models, apiKey)
	if err != nil {
		t.Fatalf("GenerateModelsJSON: %v", err)
	}
	var f gotModelsFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("emitted models.json did not parse: %v\n%s", err, b)
	}
	return f
}

// TestModelsJSONShape asserts the ONE-provider, openai-completions, endpoint
// baseUrl, placeholder-apiKey shape (acceptance: the models.json shape).
func TestModelsJSONShape(t *testing.T) {
	models := []Model{{ID: "llama3", Name: "Llama 3"}, {ID: "qwen", Name: "Qwen"}}
	f := mustModelsJSON(t, "127.0.0.1:11434", models, apikeyguard.PlaceholderAPIKey)

	if len(f.Providers) != 1 {
		t.Fatalf("want exactly ONE provider, got %d: %+v", len(f.Providers), f.Providers)
	}
	p, ok := f.Providers[LocalProviderName]
	if !ok {
		t.Fatalf("provider not keyed %q: %+v", LocalProviderName, f.Providers)
	}
	if p.API != LocalProviderAPI {
		t.Errorf("api = %q, want %q", p.API, LocalProviderAPI)
	}
	if p.APIKey != apikeyguard.PlaceholderAPIKey {
		t.Errorf("apiKey = %q, want placeholder %q", p.APIKey, apikeyguard.PlaceholderAPIKey)
	}
	if p.BaseURL != "http://127.0.0.1:11434/v1" {
		t.Errorf("baseUrl = %q, want http://127.0.0.1:11434/v1", p.BaseURL)
	}
	if len(p.Models) != 2 {
		t.Fatalf("want 2 models, got %d", len(p.Models))
	}
}

// TestModelsJSONNormalisesEndpoint: a full URL / scheme / path all normalise to
// the same host:port baseUrl (mirrors anon-pi's hostPortKey).
func TestModelsJSONNormalisesEndpoint(t *testing.T) {
	for _, ep := range []string{
		"127.0.0.1:11434",
		"http://127.0.0.1:11434/v1",
		"https://user:pass@127.0.0.1:11434/v1/models",
		"HTTP://127.0.0.1:11434",
	} {
		f := mustModelsJSON(t, ep, nil, apikeyguard.PlaceholderAPIKey)
		p := f.Providers[LocalProviderName]
		if p.BaseURL != "http://127.0.0.1:11434/v1" {
			t.Errorf("endpoint %q -> baseUrl %q, want http://127.0.0.1:11434/v1", ep, p.BaseURL)
		}
	}
}

// TestModelsJSONDedupesAndSorts: duplicate ids collapse, and entries are sorted
// by id (deterministic output).
func TestModelsJSONDedupesAndSorts(t *testing.T) {
	models := []Model{{ID: "zeta"}, {ID: "alpha"}, {ID: "alpha"}}
	f := mustModelsJSON(t, "127.0.0.1:1234", models, "none")
	ids := []string{}
	for _, m := range f.Providers[LocalProviderName].Models {
		var id string
		_ = json.Unmarshal(m["id"], &id)
		ids = append(ids, id)
	}
	if !reflect.DeepEqual(ids, []string{"alpha", "zeta"}) {
		t.Errorf("ids = %v, want [alpha zeta] (deduped + sorted)", ids)
	}
}

// TestModelsJSONPreservesConfiguredExtras: a configured (host) entry keeps its
// extra fields (contextWindow, etc.) verbatim while id/name/cost stay
// authoritative.
func TestModelsJSONPreservesConfiguredExtras(t *testing.T) {
	m := Model{
		ID:    "tuned",
		Name:  "Tuned",
		Extra: map[string]json.RawMessage{"contextWindow": json.RawMessage("32768")},
	}
	f := mustModelsJSON(t, "127.0.0.1:1234", []Model{m}, "none")
	entry := f.Providers[LocalProviderName].Models[0]
	if string(entry["contextWindow"]) != "32768" {
		t.Errorf("contextWindow not preserved: %s", entry["contextWindow"])
	}
	var name string
	_ = json.Unmarshal(entry["name"], &name)
	if name != "Tuned" {
		t.Errorf("name = %q, want Tuned", name)
	}
}

// --- pure synthesis: settings.json shape ---

func TestSettingsSelection(t *testing.T) {
	sel := GenerateModelSelection([]string{"llama3", "qwen", "llama3"}, "qwen")
	if sel.DefaultProvider != LocalProviderName {
		t.Errorf("defaultProvider = %q, want %q", sel.DefaultProvider, LocalProviderName)
	}
	if sel.DefaultModel != "qwen" {
		t.Errorf("defaultModel = %q, want qwen", sel.DefaultModel)
	}
	want := []string{"local/llama3", "local/qwen"}
	if !reflect.DeepEqual(sel.EnabledModels, want) {
		t.Errorf("enabledModels = %v, want %v (deduped, sorted, local/ prefixed)", sel.EnabledModels, want)
	}
}

func TestSettingsJSONShape(t *testing.T) {
	sel := GenerateModelSelection([]string{"m1"}, "m1")
	b, err := GenerateSettingsJSON(sel)
	if err != nil {
		t.Fatalf("GenerateSettingsJSON: %v", err)
	}
	var got struct {
		DefaultProvider string   `json:"defaultProvider"`
		DefaultModel    string   `json:"defaultModel"`
		EnabledModels   []string `json:"enabledModels"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("settings.json did not parse: %v\n%s", err, b)
	}
	if got.DefaultProvider != "local" || got.DefaultModel != "m1" ||
		!reflect.DeepEqual(got.EnabledModels, []string{"local/m1"}) {
		t.Errorf("settings.json = %+v, unexpected", got)
	}
}

// --- endpoint-scoped provider selection (anonymity-critical) ---

// multiProviderFixture is a user models.json with THREE providers: a paid remote
// (etherplay) carrying a REAL key, a google provider, and the local endpoint. Only
// the local one must ever be read.
const multiProviderFixture = `{
  "providers": {
    "etherplay": {
      "api": "anthropic-messages",
      "apiKey": "sk-ant-REAL-secret-key-do-not-leak",
      "baseUrl": "https://api.etherplay.io",
      "models": [{"id": "secret-remote-model", "name": "Secret"}]
    },
    "google": {
      "api": "openai-completions",
      "apiKey": "AIzaSyREAL-google-key",
      "baseUrl": "https://generativelanguage.googleapis.com",
      "models": [{"id": "gemini", "name": "Gemini"}]
    },
    "my-local": {
      "api": "openai-completions",
      "apiKey": "none",
      "baseUrl": "http://127.0.0.1:11434/v1",
      "models": [
        {"id": "llama3", "name": "Llama 3 (tuned)", "contextWindow": 8192},
        {"id": "mistral", "name": "Mistral"}
      ]
    }
  }
}`

func TestPickLocalProviderScopesToEndpoint(t *testing.T) {
	match, ok := pickLocalProviderModels([]byte(multiProviderFixture), "127.0.0.1:11434")
	if !ok {
		t.Fatalf("expected a match for the local endpoint")
	}
	// Only the local provider's key/models — never etherplay's or google's.
	if match.APIKey != "none" {
		t.Errorf("matched apiKey = %q, want the LOCAL provider's 'none' (not a remote key)", match.APIKey)
	}
	ids := []string{}
	for _, m := range match.Models {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"llama3", "mistral"}) {
		t.Errorf("matched models = %v, want [llama3 mistral] (only the local provider's)", ids)
	}
	// The configured extra survives.
	for _, m := range match.Models {
		if m.ID == "llama3" {
			if string(m.Extra["contextWindow"]) != "8192" {
				t.Errorf("configured contextWindow not preserved: %v", m.Extra)
			}
		}
	}
}

// TestNoLeakOfOtherProviders is the load-bearing anonymity assertion: NO field,
// key, model id, or apiKey from any non-matched provider appears anywhere in the
// generated models.json or the matched provider data.
func TestNoLeakOfOtherProviders(t *testing.T) {
	match, ok := pickLocalProviderModels([]byte(multiProviderFixture), "127.0.0.1:11434")
	if !ok {
		t.Fatalf("expected a match")
	}
	models, err := GenerateModelsJSON("127.0.0.1:11434", match.Models, apikeyguard.PlaceholderAPIKey)
	if err != nil {
		t.Fatalf("GenerateModelsJSON: %v", err)
	}
	out := string(models)

	forbidden := []string{
		"sk-ant-REAL-secret-key-do-not-leak", // etherplay real key
		"AIzaSyREAL-google-key",              // google real key
		"etherplay",
		"google",
		"secret-remote-model",
		"gemini",
		"api.etherplay.io",
		"generativelanguage.googleapis.com",
	}
	for _, f := range forbidden {
		if strings.Contains(out, f) {
			t.Errorf("LEAK: generated models.json contains non-matched provider data %q\n%s", f, out)
		}
	}
	// And it DOES contain the local provider's models + the placeholder key.
	for _, want := range []string{"llama3", "mistral", `"apiKey": "none"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected models.json to contain %q\n%s", want, out)
		}
	}
}

func TestPickLocalProviderNoMatch(t *testing.T) {
	if _, ok := pickLocalProviderModels([]byte(multiProviderFixture), "127.0.0.1:9999"); ok {
		t.Errorf("unmatched endpoint should not match any provider")
	}
	if _, ok := pickLocalProviderModels([]byte("not json"), "127.0.0.1:11434"); ok {
		t.Errorf("garbage models.json should not match")
	}
}

// --- /v1/models parsing ---

func TestParseModelsListing(t *testing.T) {
	cases := map[string]string{
		"openai data form": `{"object":"list","data":[{"id":"a"},{"id":"b"}]}`,
		"models key":       `{"models":[{"id":"a"},{"id":"b"}]}`,
		"bare array":       `[{"id":"a"},{"id":"b"}]`,
		"string array":     `["a","b"]`,
	}
	for name, body := range cases {
		got := parseModelsListing(json.RawMessage(body))
		sort.Strings(got)
		if !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Errorf("%s: parseModelsListing = %v, want [a b]", name, got)
		}
	}
	// Garbage / empty tolerated.
	for _, bad := range []string{"", "null", "{}", "garbage", `{"data":123}`} {
		if got := parseModelsListing(json.RawMessage(bad)); len(got) != 0 {
			t.Errorf("parseModelsListing(%q) = %v, want empty", bad, got)
		}
	}
}

// TestMergeModelSources: host config wins on an id present in both; server-only
// ids come in as un-configured; result is deduped + sorted.
func TestMergeModelSources(t *testing.T) {
	host := []Model{{ID: "llama3", Name: "Llama 3 (tuned)"}}
	server := []string{"llama3", "phi3"}
	cands := mergeModelSources(host, server)
	if len(cands) != 2 {
		t.Fatalf("want 2 candidates, got %d: %+v", len(cands), cands)
	}
	byID := map[string]Candidate{}
	for _, c := range cands {
		byID[c.ID] = c
	}
	if !byID["llama3"].Configured {
		t.Errorf("llama3 should be configured (from host)")
	}
	if byID["llama3"].Entry.Name != "Llama 3 (tuned)" {
		t.Errorf("host entry should win: %q", byID["llama3"].Entry.Name)
	}
	if byID["phi3"].Configured {
		t.Errorf("phi3 (server-only) should not be configured")
	}
}

// --- Plan (the pure seam) ---

func mustResolvedSeed(t *testing.T, endpoint string, models []Model, def, apiKey string) Seed {
	t.Helper()
	opts := Options{Models: models, DefaultModelID: def, APIKey: apiKey}
	opts.Endpoint = endpoint
	return New(opts)
}

func TestPlanEmitsTwoFilesAndException(t *testing.T) {
	s := mustResolvedSeed(t, "127.0.0.1:11434",
		[]Model{{ID: "llama3", Name: "Llama 3"}}, "llama3", apikeyguard.PlaceholderAPIKey)

	plan, err := s.Plan(context.Background(), seed.Options{}, seed.TargetAnonctl)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Files) != 2 {
		t.Fatalf("want 2 files, got %d: %+v", len(plan.Files), plan.Files)
	}
	paths := map[string]string{}
	for _, f := range plan.Files {
		paths[f.Path] = f.Content
	}
	if _, ok := paths[ModelsFilePath]; !ok {
		t.Errorf("missing %s; got %v", ModelsFilePath, keysOf(paths))
	}
	if _, ok := paths[SettingsFilePath]; !ok {
		t.Errorf("missing %s; got %v", SettingsFilePath, keysOf(paths))
	}
	// Exactly one exception, the endpoint's host:port.
	if len(plan.Exceptions) != 1 {
		t.Fatalf("want 1 exception, got %d: %+v", len(plan.Exceptions), plan.Exceptions)
	}
	if plan.Exceptions[0].Allow != "127.0.0.1:11434" {
		t.Errorf("exception allow = %q, want 127.0.0.1:11434", plan.Exceptions[0].Allow)
	}
	// The models.json carries the placeholder key.
	if !strings.Contains(paths[ModelsFilePath], `"apiKey": "none"`) {
		t.Errorf("models.json missing placeholder apiKey:\n%s", paths[ModelsFilePath])
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	s := mustResolvedSeed(t, "127.0.0.1:1234",
		[]Model{{ID: "b"}, {ID: "a"}}, "a", "none")
	p1, err := s.Plan(context.Background(), seed.Options{}, seed.TargetAnonctl)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	p2, err := s.Plan(context.Background(), seed.Options{}, seed.TargetAnonctl)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !reflect.DeepEqual(p1, p2) {
		t.Errorf("Plan not deterministic")
	}
}

func TestPlanRejectsUnresolvedSeed(t *testing.T) {
	var zero Seed // no Resolve/New
	if _, err := zero.Plan(context.Background(), seed.Options{Endpoint: "127.0.0.1:1"}, seed.TargetAnonctl); err == nil {
		t.Errorf("Plan on an unresolved seed should error (it cannot invent picks)")
	}
}

func TestPlanEndpointFallsBackToSeedOptions(t *testing.T) {
	// Carried Options have no endpoint; the generic seed.Options supplies it.
	opts := Options{Models: []Model{{ID: "m"}}, DefaultModelID: "m", APIKey: "none"}
	s := New(opts)
	plan, err := s.Plan(context.Background(), seed.Options{Endpoint: "127.0.0.1:5555"}, seed.TargetAnonctl)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Exceptions[0].Allow != "127.0.0.1:5555" {
		t.Errorf("endpoint not taken from seed.Options: %q", plan.Exceptions[0].Allow)
	}
}

func TestSeedMetadata(t *testing.T) {
	var s Seed
	if s.Name() != "pi" {
		t.Errorf("Name = %q, want pi", s.Name())
	}
	if !reflect.DeepEqual(s.Targets(), []seed.Target{seed.TargetAnonctl}) {
		t.Errorf("Targets = %v, want [anonctl]", s.Targets())
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
