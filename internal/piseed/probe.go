// probe.go holds the pi seed's INTERACTIVE half: the UPSTREAM-of-Plan step that
// probes the endpoint's live /v1/models, reads the endpoint-matched provider from
// the user's own ~/.pi/agent/models.json, runs the api-key guard, and lets the
// operator pick which models to import and the default. It resolves everything
// into a pure piseed.Options that Plan (the pure half, piseed.go) consumes.
//
// Every impure edge is behind an injectable seam so tests drive it with a fake
// HTTP client and a fixture models.json, writing only to temp:
//
//   - ProbeFunc: fetch the endpoint's /v1/models body (a fake HTTP server in
//     tests; a real net/http client in production).
//   - ReadUserModelsFunc: read the user's models.json bytes (a fixture file in
//     tests; os.ReadFile of the resolved ~/.pi/agent/models.json in production).
//   - PickFunc: choose which candidates to import + the default (a scripted pick
//     in tests; an interactive TUI in production).
//
// The api-key guard (apikeyguard.Guard) runs on the MATCHED provider's key BEFORE
// it can enter Options, so a real key is refused unless forced. Only the matched
// provider's key is ever seen (pickLocalProviderModels scopes to the endpoint), so
// no other provider's credential can reach the guard, let alone the plan.
package piseed

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wighawag/anonseed/internal/apikeyguard"
)

// ProbeFunc fetches the endpoint's live /v1/models response body. The endpoint is
// the resolved `host:port`; the implementation builds the URL
// (http://host:port/v1/models) and returns the raw JSON body. A probe error is
// NON-FATAL to Resolve: the seed falls back to the user-config models alone (the
// server was unreachable but the hand-tuned config is still valid), so Resolve
// records the error rather than aborting. In tests this is a fake HTTP server.
type ProbeFunc func(ctx context.Context, endpoint string) (json.RawMessage, error)

// ReadUserModelsFunc reads the user's own ~/.pi/agent/models.json bytes. A
// not-found / unreadable file is NON-FATAL (returns nil, nil-or-error): the seed
// then relies on the live probe alone. In tests this reads a fixture file.
type ReadUserModelsFunc func() ([]byte, error)

// Pick is the operator's choice: the model IDs to import and which is the default.
// The default must be one of the imported IDs (Resolve validates this).
type Pick struct {
	// ImportIDs is the set of candidate IDs the operator chose to import. Empty is
	// allowed (the degraded model-only-endpoint fallback, no pickable model).
	ImportIDs []string
	// DefaultID is the chosen default model's ID (one of ImportIDs), or "" when
	// ImportIDs is empty.
	DefaultID string
}

// PickFunc lets the operator choose which candidates to import and the default,
// given the merged candidate list. In tests this is a scripted pick; in
// production an interactive TUI. It may return an error to abort the seed.
type PickFunc func(candidates []Candidate) (Pick, error)

// ResolveInput bundles the seams + inputs the interactive Resolve needs. Endpoint
// is the resolved `host:port`; Force is the operator's explicit
// --force-allow-local-llm-api-key (passed to the api-key guard). Probe / ReadUserModels
// / Pick are the injectable impure edges (all required).
type ResolveInput struct {
	Endpoint string
	Force    bool

	Probe          ProbeFunc
	ReadUserModels ReadUserModelsFunc
	Pick           PickFunc

	// DetectSearxng is the SearXNG-detection seam for the default-on webveil wiring
	// (production sniffs the host; tests inject a fixture). It is OPTIONAL: a nil
	// DetectSearxng means "no detection", equivalent to SearXNG absent, so a caller
	// that does not wire webveil (e.g. a model-only path) can leave it unset. A
	// detection error is treated as "not detected" (non-fatal), so a probe of the
	// host never aborts the seed.
	DetectSearxng DetectSearxngFunc

	// Webveil is the operator's resolved webveil decision (the disable flag /
	// socket override / accept-install-default choice). It is applied through
	// ResolveWebveil against DetectSearxng's result: webveil is default-ON, so the
	// zero WebveilChoice with a detected SearXNG wires webveil, and Webveil.Disabled
	// is the one way to turn it off. When SearXNG is absent and the operator did not
	// accept the install default, the result is the explicit model-only fallback.
	Webveil WebveilChoice
}

// Resolve is the pi seed's interactive UPSTREAM step: it probes the endpoint's
// live /v1/models, reads the endpoint-matched provider from the user's
// models.json, runs the api-key guard on the matched key, presents the merged
// candidates to the operator, and returns a pure piseed.Options for Plan.
//
// The anonymity-critical scoping is structural: pickLocalProviderModels returns
// ONLY the provider whose baseUrl matches the endpoint, so no other provider's
// models OR key are ever read into the candidate list or the guard. The guard
// runs BEFORE the key enters Options: a real-looking matched key is refused
// (apikeyguard.ErrRealAPIKey) unless Force is set; the benign/placeholder case (or
// an unmatched provider, which contributes no key) passes and the seed writes the
// benign placeholder.
//
// Resolve performs I/O ONLY through the injected seams; it is otherwise pure
// orchestration. A probe or user-config read error is non-fatal (the other source
// still yields candidates); a Pick error, an empty-candidate-with-nonempty-pick
// mismatch, or a refused key aborts.
func Resolve(ctx context.Context, in ResolveInput) (Options, error) {
	endpoint := strings.TrimSpace(in.Endpoint)
	if endpoint == "" {
		return Options{}, fmt.Errorf("piseed: Resolve needs a non-empty endpoint")
	}
	if in.Probe == nil || in.ReadUserModels == nil || in.Pick == nil {
		return Options{}, fmt.Errorf("piseed: Resolve needs Probe, ReadUserModels and Pick seams")
	}

	// The endpoint-matched provider from the user's own config (the ONLY provider
	// considered). A read error or no match leaves us with no host models + the
	// benign placeholder key; the guard still runs (on the empty/absent key, which
	// is benign) so the code path is uniform.
	var hostModels []Model
	matchedKey := apikeyguard.PlaceholderAPIKey
	if raw, err := in.ReadUserModels(); err == nil && len(raw) > 0 {
		if match, ok := pickLocalProviderModels(raw, endpoint); ok {
			hostModels = match.Models
			matchedKey = match.APIKey
		}
	}

	// The api-key guard runs on the MATCHED key BEFORE it can enter Options. A
	// real-looking key is refused unless Force; a benign/placeholder key passes.
	// This is the one seam every candidate key funnels through, so the invariant
	// "a seeded home never carries a real credential" holds by construction.
	if err := apikeyguard.Guard(matchedKey, in.Force); err != nil {
		return Options{}, fmt.Errorf("piseed: matched provider apiKey refused: %w", err)
	}

	// A local model ignores its apiKey, so the seed writes the benign placeholder
	// by DEFAULT even when the matched key is benign-but-nonempty (e.g. "ollama"):
	// the anonymized home should carry the neutral value, not the host's chosen
	// placeholder. When Force carried a real key through, that explicit operator
	// override is written verbatim (they asked for it).
	seededKey := apikeyguard.PlaceholderAPIKey
	if in.Force && apikeyguard.LooksReal(matchedKey) {
		seededKey = matchedKey
	}

	// The live /v1/models ids (non-fatal on error: the host config alone still
	// yields candidates).
	var serverIDs []string
	if body, err := in.Probe(ctx, endpoint); err == nil {
		serverIDs = parseModelsListing(body)
	}

	// webveil (default-on, disable-able): detect a host SearXNG (non-fatal on
	// error / when the seam is unset -> absent) and apply the operator's decision
	// tree. The result is *WebveilOptions on Options: non-nil wires webveil at the
	// resolved socket, nil is the explicit model-only fallback (Webveil.Disabled or
	// no SearXNG + not accepting the install default).
	var detection SearxngDetection
	if in.DetectSearxng != nil {
		if d, derr := in.DetectSearxng(); derr == nil {
			detection = d
		}
	}
	webveil := ResolveWebveil(detection, in.Webveil)

	candidates := mergeModelSources(hostModels, serverIDs)

	pick, err := in.Pick(candidates)
	if err != nil {
		return Options{}, fmt.Errorf("piseed: model pick aborted: %w", err)
	}

	models, err := selectPicked(candidates, pick)
	if err != nil {
		return Options{}, err
	}

	opts := Options{
		Models:         models,
		DefaultModelID: strings.TrimSpace(pick.DefaultID),
		APIKey:         seededKey,
		Webveil:        webveil,
	}
	opts.Endpoint = endpoint
	return opts, nil
}

// selectPicked turns a Pick (chosen IDs + default) into the ordered set of Model
// entries to import, pulling each ID's full entry from the candidate list. A
// picked ID absent from the candidates is an error (the operator picked something
// not offered); a non-empty pick with an empty or non-member default is an error.
// An empty pick yields no models (the degraded fallback), which requires an empty
// default.
func selectPicked(candidates []Candidate, pick Pick) ([]Model, error) {
	byID := map[string]Model{}
	for _, c := range candidates {
		byID[c.ID] = c.Entry
	}

	models := make([]Model, 0, len(pick.ImportIDs))
	chosen := map[string]struct{}{}
	for _, raw := range pick.ImportIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, dup := chosen[id]; dup {
			continue
		}
		entry, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("piseed: picked model %q was not among the offered candidates", id)
		}
		chosen[id] = struct{}{}
		models = append(models, entry)
	}

	def := strings.TrimSpace(pick.DefaultID)
	if len(models) == 0 {
		if def != "" {
			return nil, fmt.Errorf("piseed: a default model %q was chosen but no models were imported", def)
		}
		return models, nil
	}
	if def == "" {
		return nil, fmt.Errorf("piseed: models were imported but no default model was chosen")
	}
	if _, ok := chosen[def]; !ok {
		return nil, fmt.Errorf("piseed: default model %q is not among the imported models", def)
	}
	return models, nil
}
