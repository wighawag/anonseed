package piseed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wighawag/anonseed/internal/apikeyguard"
)

// fakeModelsServer stands up a real HTTP server serving a /v1/models body, so the
// probe seam is exercised end to end (a fake HTTP endpoint, not a mock). It
// returns the endpoint host:port the caller passes to Resolve.
func fakeModelsServer(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	// strip scheme -> host:port (the endpoint the seed reasons about).
	return strings.TrimPrefix(srv.URL, "http://")
}

// httpProbe is the production-shaped probe: GET http://<endpoint>/v1/models. It is
// the seam Resolve calls; the test injects it pointed at the fake server.
func httpProbe(ctx context.Context, endpoint string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+endpoint+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var buf [1 << 16]byte
	n, _ := resp.Body.Read(buf[:])
	return json.RawMessage(buf[:n]), nil
}

// writeFixture writes a user models.json into a temp dir and returns a
// ReadUserModelsFunc reading it (the file-read seam; tests write only to temp).
func writeFixture(t *testing.T, content string) ReadUserModelsFunc {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return func() ([]byte, error) { return os.ReadFile(path) }
}

// pickAll imports every candidate and defaults to the first (sorted) id.
func pickAll(candidates []Candidate) (Pick, error) {
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	sort.Strings(ids)
	def := ""
	if len(ids) > 0 {
		def = ids[0]
	}
	return Pick{ImportIDs: ids, DefaultID: def}, nil
}

// TestResolveEndToEnd: fake /v1/models server + multi-provider fixture. Resolve
// scopes to the local endpoint, merges server + configured models, and the
// resulting Plan emits the two files + the exception, carrying ONLY the local
// provider's data and the placeholder key.
func TestResolveEndToEnd(t *testing.T) {
	endpoint := fakeModelsServer(t, `{"data":[{"id":"llama3"},{"id":"server-only"}]}`)
	// Point the fixture's local provider at the fake server's host:port.
	fixture := `{"providers":{
	  "remote":{"apiKey":"sk-REAL-do-not-leak","baseUrl":"https://api.remote.example","models":[{"id":"remote-secret"}]},
	  "local":{"apiKey":"none","baseUrl":"http://` + endpoint + `/v1","models":[{"id":"llama3","name":"Llama 3 (tuned)","contextWindow":8192}]}
	}}`

	opts, err := Resolve(context.Background(), ResolveInput{
		Endpoint:       endpoint,
		Probe:          httpProbe,
		ReadUserModels: writeFixture(t, fixture),
		Pick:           pickAll,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if opts.APIKey != apikeyguard.PlaceholderAPIKey {
		t.Errorf("seeded apiKey = %q, want placeholder", opts.APIKey)
	}
	ids := []string{}
	for _, m := range opts.Models {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	// llama3 (configured + server) and server-only (server); NOT remote-secret.
	if !reflect.DeepEqual(ids, []string{"llama3", "server-only"}) {
		t.Fatalf("imported ids = %v, want [llama3 server-only]", ids)
	}

	plan, err := New(opts).Plan(context.Background(), opts.Options, "anonctl")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	var modelsContent, settingsContent string
	for _, f := range plan.Files {
		switch f.Path {
		case ModelsFilePath:
			modelsContent = f.Content
		case SettingsFilePath:
			settingsContent = f.Content
		}
	}
	// No leak of the remote provider.
	for _, leak := range []string{"sk-REAL-do-not-leak", "remote-secret", "api.remote.example", "remote"} {
		if strings.Contains(modelsContent, leak) || strings.Contains(settingsContent, leak) {
			t.Errorf("LEAK of non-matched provider %q\nmodels:%s\nsettings:%s", leak, modelsContent, settingsContent)
		}
	}
	// Configured extra preserved.
	if !strings.Contains(modelsContent, "8192") {
		t.Errorf("configured contextWindow not preserved:\n%s", modelsContent)
	}
	// The single exception is the endpoint.
	if len(plan.Exceptions) != 1 || plan.Exceptions[0].Allow != endpoint {
		t.Errorf("exception = %+v, want single %q", plan.Exceptions, endpoint)
	}
}

// TestResolveRefusesRealKey: the matched provider carries a REAL apiKey. Without
// force, Resolve REFUSES (apikeyguard.ErrRealAPIKey), and no plan is produced.
func TestResolveRefusesRealKey(t *testing.T) {
	endpoint := fakeModelsServer(t, `{"data":[{"id":"m"}]}`)
	fixture := `{"providers":{"local":{"apiKey":"sk-ant-REAL","baseUrl":"http://` + endpoint + `/v1","models":[{"id":"m"}]}}}`

	_, err := Resolve(context.Background(), ResolveInput{
		Endpoint:       endpoint,
		Force:          false,
		Probe:          httpProbe,
		ReadUserModels: writeFixture(t, fixture),
		Pick:           pickAll,
	})
	if err == nil {
		t.Fatalf("Resolve should REFUSE a real matched apiKey without force")
	}
	var real *apikeyguard.ErrRealAPIKey
	if !errors.As(err, &real) {
		t.Errorf("error should wrap apikeyguard.ErrRealAPIKey, got %v", err)
	}
}

// TestResolveForcedRealKeyPasses: with force, the real matched key is carried
// through verbatim (the operator's explicit, auditable override).
func TestResolveForcedRealKeyPasses(t *testing.T) {
	endpoint := fakeModelsServer(t, `{"data":[{"id":"m"}]}`)
	fixture := `{"providers":{"local":{"apiKey":"sk-ant-REAL","baseUrl":"http://` + endpoint + `/v1","models":[{"id":"m"}]}}}`

	opts, err := Resolve(context.Background(), ResolveInput{
		Endpoint:       endpoint,
		Force:          true,
		Probe:          httpProbe,
		ReadUserModels: writeFixture(t, fixture),
		Pick:           pickAll,
	})
	if err != nil {
		t.Fatalf("Resolve with force: %v", err)
	}
	if opts.APIKey != "sk-ant-REAL" {
		t.Errorf("forced real key should be carried verbatim, got %q", opts.APIKey)
	}
}

// TestResolveBenignKeySeedsPlaceholder: a benign-but-nonempty matched key (e.g.
// "ollama") still seeds the neutral placeholder, not the host's chosen value.
func TestResolveBenignKeySeedsPlaceholder(t *testing.T) {
	endpoint := fakeModelsServer(t, `{"data":[{"id":"m"}]}`)
	fixture := `{"providers":{"local":{"apiKey":"ollama","baseUrl":"http://` + endpoint + `/v1","models":[{"id":"m"}]}}}`

	opts, err := Resolve(context.Background(), ResolveInput{
		Endpoint:       endpoint,
		Probe:          httpProbe,
		ReadUserModels: writeFixture(t, fixture),
		Pick:           pickAll,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if opts.APIKey != apikeyguard.PlaceholderAPIKey {
		t.Errorf("benign nonempty key should seed placeholder, got %q", opts.APIKey)
	}
}

// TestResolveProbeErrorNonFatal: an unreachable endpoint (probe fails) still
// yields the host-configured models. The interactive read seam is the fallback.
func TestResolveProbeErrorNonFatal(t *testing.T) {
	fixture := `{"providers":{"local":{"apiKey":"none","baseUrl":"http://127.0.0.1:59999/v1","models":[{"id":"only-configured"}]}}}`
	failProbe := func(context.Context, string) (json.RawMessage, error) {
		return nil, errors.New("connection refused")
	}
	opts, err := Resolve(context.Background(), ResolveInput{
		Endpoint:       "127.0.0.1:59999",
		Probe:          failProbe,
		ReadUserModels: writeFixture(t, fixture),
		Pick:           pickAll,
	})
	if err != nil {
		t.Fatalf("probe failure should be non-fatal: %v", err)
	}
	if len(opts.Models) != 1 || opts.Models[0].ID != "only-configured" {
		t.Errorf("want the host-configured model despite probe failure, got %+v", opts.Models)
	}
}

// TestResolveNoUserConfigStillWorks: no user models.json (read returns nil). The
// live probe alone yields candidates, the key is the benign placeholder.
func TestResolveNoUserConfigStillWorks(t *testing.T) {
	endpoint := fakeModelsServer(t, `{"data":[{"id":"srv-a"},{"id":"srv-b"}]}`)
	noConfig := func() ([]byte, error) { return nil, os.ErrNotExist }

	opts, err := Resolve(context.Background(), ResolveInput{
		Endpoint:       endpoint,
		Probe:          httpProbe,
		ReadUserModels: noConfig,
		Pick:           pickAll,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if opts.APIKey != apikeyguard.PlaceholderAPIKey {
		t.Errorf("apiKey = %q, want placeholder", opts.APIKey)
	}
	ids := []string{}
	for _, m := range opts.Models {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"srv-a", "srv-b"}) {
		t.Errorf("server-only ids = %v, want [srv-a srv-b]", ids)
	}
}

// TestResolveEmptyPickDegradedFallback: an empty pick (no models chosen) is the
// degraded model-only-endpoint fallback: valid, empty models, empty default.
func TestResolveEmptyPickDegradedFallback(t *testing.T) {
	endpoint := fakeModelsServer(t, `{"data":[{"id":"m"}]}`)
	pickNone := func([]Candidate) (Pick, error) { return Pick{}, nil }
	opts, err := Resolve(context.Background(), ResolveInput{
		Endpoint:       endpoint,
		Probe:          httpProbe,
		ReadUserModels: func() ([]byte, error) { return nil, nil },
		Pick:           pickNone,
	})
	if err != nil {
		t.Fatalf("empty pick should be valid: %v", err)
	}
	if len(opts.Models) != 0 || opts.DefaultModelID != "" {
		t.Errorf("degraded fallback should carry no models/default, got %+v", opts)
	}
}

// TestSelectPickedValidation: a picked-but-unoffered id, a default not among
// imports, and an imported-set-with-no-default are all errors.
func TestSelectPickedValidation(t *testing.T) {
	cands := []Candidate{{ID: "a", Entry: Model{ID: "a"}}, {ID: "b", Entry: Model{ID: "b"}}}
	cases := []struct {
		name string
		pick Pick
	}{
		{"unoffered id", Pick{ImportIDs: []string{"a", "zzz"}, DefaultID: "a"}},
		{"default not imported", Pick{ImportIDs: []string{"a"}, DefaultID: "b"}},
		{"imported but no default", Pick{ImportIDs: []string{"a"}, DefaultID: ""}},
		{"default without imports", Pick{ImportIDs: nil, DefaultID: "a"}},
	}
	for _, c := range cases {
		if _, err := selectPicked(cands, c.pick); err == nil {
			t.Errorf("%s: expected an error", c.name)
		}
	}
}

// TestResolveRejectsBadInputs guards the seam preconditions.
func TestResolveRejectsBadInputs(t *testing.T) {
	if _, err := Resolve(context.Background(), ResolveInput{Endpoint: ""}); err == nil {
		t.Errorf("empty endpoint should error")
	}
	if _, err := Resolve(context.Background(), ResolveInput{Endpoint: "127.0.0.1:1"}); err == nil {
		t.Errorf("missing seams should error")
	}
}
