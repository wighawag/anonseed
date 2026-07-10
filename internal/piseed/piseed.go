// Package piseed is anonseed's first built-in seed type: the `pi` seed's
// model-config half. It mirrors the sibling tool anon-pi's `init` (anon-pi, a
// pi-specific launcher, is being retired; its provisioning knowledge becomes
// anonseed's). Given a local model endpoint's `host:port`, it synthesises the two
// pi config files (`models.json` + `settings.json`) for the target home's
// `~/.pi/agent/` plus the one direct-egress `--allow` exception the endpoint
// needs, and nothing else.
//
// The flow is SPLIT into a pure half and an interactive half so Plan stays
// deterministic (seed.Seed's load-bearing invariant):
//
//   - The PURE half (this file): the synthesis functions (parseModelsListing,
//     pickLocalProviderModels, mergeModelSources, GenerateModelsJSON,
//     GenerateModelSelection) and the Seed implementation whose Plan turns
//     already-resolved picks into a seed.SeedPlan with NO filesystem or network
//     I/O. Plan takes the picks as plain data on Options.
//   - The INTERACTIVE half (probe.go): probing the endpoint's live `/v1/models`
//     (behind an HTTP seam), reading the endpoint-matched provider from the
//     user's own `~/.pi/agent/models.json` (behind a file seam), and letting the
//     operator pick which models to import and the default. It resolves the picks
//     UPSTREAM of Plan.
//
// # The anonymity-critical scoping (by construction)
//
// pickLocalProviderModels considers ONLY the provider in the user's models.json
// whose baseUrl matches the endpoint (matched via hostPortKey, the same
// normalisation the `--allow` target uses). No other provider, and no other
// provider's apiKey, can enter the seed: the scoping is structural, not a filter
// applied late. This is the whole point of anonseed (a jailed identity carrying
// the operator's real credential still authenticates AS the operator).
//
// # The api-key guard
//
// Before the matched provider's apiKey enters the plan, the interactive half runs
// anonseed's apikeyguard (refuse a real-looking key unless forced). A genuinely
// local model ignores its apiKey, so the seed defaults to the benign placeholder
// (apikeyguard.PlaceholderAPIKey); a real key is refused loudly. Plan itself is
// pure and just writes the (already-guarded) key it is given.
package piseed

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/wighawag/anonseed/internal/seed"
)

// LocalProviderName is the neutral, host-agnostic provider key the pi seed gives
// the single local provider it generates. It carries NO host identity (mirrors
// anon-pi's LOCAL_PROVIDER_NAME), so the seeded models.json cannot be
// fingerprinted back to the operator's own provider naming.
const LocalProviderName = "local"

// LocalProviderAPI is the pi `api` dialect the generated local provider speaks.
// Local model servers (llama.cpp, ollama, LM Studio, vLLM, ...) are
// overwhelmingly OpenAI-compatible and serve the completions API under `/v1`, so
// this is the safe default for a captured endpoint. Mirrors anon-pi's
// LOCAL_PROVIDER_API.
const LocalProviderAPI = "openai-completions"

// Model is a pi model entry as the seed writes it for the local provider. pi keys
// a model by ID; Name is the display label and Cost is all-zero (a LAN model is
// free). A "configured" entry (imported from the user's models.json) preserves
// whatever extra fields it carried (contextWindow, maxTokens, reasoning, ...) via
// Extra; a "server"-sourced entry is the minimal id/name/cost. Mirrors anon-pi's
// GeneratedModel.
type Model struct {
	ID   string
	Name string
	Cost Cost
	// Extra holds any fields a configured host entry carried beyond id/name/cost,
	// preserved verbatim so a hand-tuned model keeps its real settings. It is nil
	// for a minimal server-sourced entry.
	Extra map[string]json.RawMessage
}

// Cost is a pi model's per-token cost. A LAN model is free, so the seed writes
// all-zero costs; the field exists so the emitted JSON matches pi's schema.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// localModelEntry turns a discovered model ID into a minimal-but-valid pi model
// entry. Name defaults to the ID; a LAN model is free, so every cost is 0.
// Mirrors anon-pi's localModelEntry.
func localModelEntry(id string) Model {
	return Model{ID: id, Name: id, Cost: Cost{}}
}

// MarshalJSON emits a Model as pi expects it under a provider's `models` list:
// id/name/cost plus any preserved Extra fields, flattened into one object. Extra
// keys never override id/name/cost (those are authoritative on the seeded entry).
func (m Model) MarshalJSON() ([]byte, error) {
	obj := map[string]json.RawMessage{}
	for k, v := range m.Extra {
		obj[k] = v
	}
	id, err := json.Marshal(m.ID)
	if err != nil {
		return nil, err
	}
	name, err := json.Marshal(m.Name)
	if err != nil {
		return nil, err
	}
	cost, err := json.Marshal(m.Cost)
	if err != nil {
		return nil, err
	}
	obj["id"] = id
	obj["name"] = name
	obj["cost"] = cost
	return json.Marshal(obj)
}

// Candidate is a model offered to the picker. Configured means it came from the
// endpoint-matched provider in the user's models.json (a hand-tuned entry with
// its real config); otherwise it was only reported by the endpoint's
// `/v1/models` (a bare id we synthesised a minimal entry for). Every candidate is
// served by the endpoint, so every one is `--allow`-reachable. Mirrors anon-pi's
// ModelCandidate.
type Candidate struct {
	ID         string
	Configured bool
	Entry      Model
}

// hostPortKey normalises an endpoint or a provider baseUrl to a lowercase
// `host:port` (dropping any scheme, path, and user:pass@), so a URL, a bare
// host:port, and a baseUrl all compare equal for the same endpoint. It is the
// SAME normalisation the `--allow` target and the provider match both go through,
// mirroring anon-pi's hostPortKey.
func hostPortKey(value string) string {
	v := strings.TrimSpace(value)
	if i := strings.Index(v, "://"); i >= 0 {
		v = v[i+3:]
	}
	if i := strings.IndexByte(v, '/'); i >= 0 {
		v = v[:i] // drop path (/v1, ...)
	}
	if i := strings.LastIndexByte(v, '@'); i >= 0 {
		v = v[i+1:] // drop any user:pass@
	}
	return strings.ToLower(v)
}

// parseModelsListing extracts the model IDs from a parsed OpenAI-compatible
// `/v1/models` response (`{ "data": [{ "id" }, ...] }`, as llama.cpp / vLLM / LM
// Studio serve). It tolerates a bare array, a `models` key, and missing/garbage
// input (returns an empty slice), so the caller can feed whatever the endpoint
// returned straight in. Mirrors anon-pi's parseModelsListing.
func parseModelsListing(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	// Try the object form first ({data|models: [...]}), else a bare array.
	var obj struct {
		Data   []json.RawMessage `json:"data"`
		Models []json.RawMessage `json:"models"`
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		switch {
		case obj.Data != nil:
			rows = obj.Data
		case obj.Models != nil:
			rows = obj.Models
		}
	}
	if rows == nil {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err == nil {
			rows = arr
		}
	}

	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		var s string
		if err := json.Unmarshal(r, &s); err == nil {
			if t := strings.TrimSpace(s); t != "" {
				ids = append(ids, t)
			}
			continue
		}
		var m struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(r, &m); err == nil {
			if t := strings.TrimSpace(m.ID); t != "" {
				ids = append(ids, t)
			}
		}
	}
	return ids
}

// ProviderMatch is the result of scanning the user's models.json for the
// endpoint's provider: ONLY that provider's models + apiKey. Mirrors anon-pi's
// HostProviderMatch. The APIKeyLooksReal flag is computed by the caller via
// apikeyguard; this struct just carries the matched key verbatim for that check.
type ProviderMatch struct {
	// Models is the matched provider's models as full pi entries (verbatim host
	// config, so a hand-tuned entry keeps its real settings).
	Models []Model
	// APIKey is the matched provider's apiKey verbatim, for the benign/real check.
	APIKey string
}

// piModelsFile is the minimal parsed shape of a user's models.json the scan
// reads. Only providers[name].{baseUrl,apiKey,models} are typed; everything else
// is ignored (never copied), so no unrelated field can ride along.
type piModelsFile struct {
	Providers map[string]piProvider `json:"providers"`
}

type piProvider struct {
	BaseURL string            `json:"baseUrl"`
	APIKey  string            `json:"apiKey"`
	Models  []json.RawMessage `json:"models"`
}

// pickLocalProviderModels finds, in a parsed user `~/.pi/agent/models.json`, the
// provider whose baseUrl points at endpoint (matched via hostPortKey) and returns
// ONLY that provider's models + apiKey. This is the anonymity-critical scoping:
// the ONLY provider considered is the one served by the `--allow` endpoint, so no
// other provider (and no other provider's key) can ever enter the seed. It
// returns (match, true) on a hit and (zero, false) when no provider matches.
// Mirrors anon-pi's pickLocalProviderModels.
//
// It is PURE: it takes the ALREADY-READ file bytes (the file read is the
// interactive half's seam) and does no I/O. Malformed JSON yields no match.
func pickLocalProviderModels(userModelsJSON []byte, endpoint string) (ProviderMatch, bool) {
	var file piModelsFile
	if err := json.Unmarshal(userModelsJSON, &file); err != nil {
		return ProviderMatch{}, false
	}
	want := hostPortKey(endpoint)

	// Iterate provider names in sorted order so a (degenerate) config with two
	// providers on the same endpoint resolves deterministically.
	names := make([]string, 0, len(file.Providers))
	for name := range file.Providers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		p := file.Providers[name]
		if strings.TrimSpace(p.BaseURL) == "" {
			continue
		}
		if hostPortKey(p.BaseURL) != want {
			continue
		}
		models := make([]Model, 0, len(p.Models))
		for _, raw := range p.Models {
			if m, ok := parseHostModel(raw); ok {
				models = append(models, m)
			}
		}
		return ProviderMatch{Models: models, APIKey: p.APIKey}, true
	}
	return ProviderMatch{}, false
}

// parseHostModel turns one raw model entry from the user's config into a Model,
// preserving every field beyond id/name/cost via Extra. A bare string entry
// becomes a minimal entry. An entry with no usable id is dropped.
func parseHostModel(raw json.RawMessage) (Model, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if t := strings.TrimSpace(s); t != "" {
			return localModelEntry(t), true
		}
		return Model{}, false
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Model{}, false
	}
	var id string
	if v, ok := obj["id"]; ok {
		_ = json.Unmarshal(v, &id)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Model{}, false
	}
	m := Model{ID: id, Name: id}
	if v, ok := obj["name"]; ok {
		var name string
		if json.Unmarshal(v, &name) == nil && strings.TrimSpace(name) != "" {
			m.Name = strings.TrimSpace(name)
		}
	}
	if v, ok := obj["cost"]; ok {
		_ = json.Unmarshal(v, &m.Cost)
	}
	// Preserve every field except the three authoritative ones (id/name/cost),
	// so a hand-tuned entry keeps contextWindow/maxTokens/reasoning/... verbatim.
	extra := map[string]json.RawMessage{}
	for k, v := range obj {
		switch k {
		case "id", "name", "cost":
			continue
		}
		extra[k] = v
	}
	if len(extra) > 0 {
		m.Extra = extra
	}
	return m, true
}

// mergeModelSources merges the endpoint-matched host-config models (rich,
// configured) with the endpoint's live `/v1/models` IDs (configured==false for
// any the host did not already carry), into ONE deduped, ID-sorted candidate
// list. Host config wins on an ID present in both (it has the real config). Every
// candidate is served by the endpoint, so every one is `--allow`-reachable.
// Mirrors anon-pi's mergeModelSources.
func mergeModelSources(hostModels []Model, serverIDs []string) []Candidate {
	byID := map[string]Candidate{}
	order := []string{}
	for _, m := range hostModels {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		entry := m
		entry.ID = id
		byID[id] = Candidate{ID: id, Configured: true, Entry: entry}
	}
	for _, raw := range serverIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, seen := byID[id]; seen {
			continue
		}
		order = append(order, id)
		byID[id] = Candidate{ID: id, Configured: false, Entry: localModelEntry(id)}
	}
	out := make([]Candidate, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GenerateModelsJSON synthesises a pi models.json for the local provider from the
// endpoint and the CHOSEN model entries. It normalises the endpoint with
// hostPortKey and emits exactly ONE provider (named LocalProviderName, a neutral
// name with no host fingerprint) pointed at that endpoint, api openai-completions,
// baseUrl `http://<host:port>/v1`, and the given apiKey. Models are deduped by ID
// and sorted. Mirrors anon-pi's generateModelsJson.
//
// It is PURE. apiKey is written verbatim: the benign/real decision (and the
// refusal) lives UPSTREAM, in the interactive half via apikeyguard, so a real key
// never reaches here without an explicit force. Callers pass
// apikeyguard.PlaceholderAPIKey for the normal local-model case.
func GenerateModelsJSON(endpoint string, models []Model, apiKey string) ([]byte, error) {
	hostPort := hostPortKey(endpoint)
	entries := make([]Model, 0, len(models))
	seen := map[string]struct{}{}
	for _, m := range models {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		e := m
		e.ID = id
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	// A stable, explicit map ordering is not needed here (one provider), and
	// json.Marshal sorts map keys, so the output is deterministic.
	out := map[string]any{
		"providers": map[string]any{
			LocalProviderName: map[string]any{
				"api":     LocalProviderAPI,
				"apiKey":  apiKey,
				"baseUrl": fmt.Sprintf("http://%s/v1", hostPort),
				"models":  entries,
			},
		},
	}
	return marshalIndent(out)
}

// ModelSelection is the pi settings.json fragment the seed sets for the
// local-model default: defaultProvider, defaultModel, enabledModels. Mirrors
// anon-pi's ModelSelection.
type ModelSelection struct {
	DefaultProvider string   `json:"defaultProvider"`
	DefaultModel    string   `json:"defaultModel"`
	EnabledModels   []string `json:"enabledModels"`

	// Packages are the pi extension sources the seeded home declares (e.g.
	// "npm:pi-webveil" when webveil is wired), so pi materialises them on first run.
	// omitempty: a model-only seed writes no `packages` key at all (unchanged from
	// before this field existed), so plain model seeds are byte-identical.
	Packages []string `json:"packages,omitempty"`
}

// GenerateModelSelection builds the settings.json selection fragment for the
// seeded local provider: defaultProvider = LocalProviderName, defaultModel = the
// chosen default id, enabledModels = `local/<id>` for each imported model (pi's
// `<provider>/<id>` convention), deduped and sorted. Mirrors anon-pi's
// generateModelSelection. PURE.
func GenerateModelSelection(modelIDs []string, defaultID string) ModelSelection {
	seen := map[string]struct{}{}
	ids := []string{}
	for _, raw := range modelIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	enabled := make([]string, 0, len(ids))
	for _, id := range ids {
		enabled = append(enabled, LocalProviderName+"/"+id)
	}
	return ModelSelection{
		DefaultProvider: LocalProviderName,
		DefaultModel:    strings.TrimSpace(defaultID),
		EnabledModels:   enabled,
	}
}

// GenerateSettingsJSON serialises a ModelSelection as the seeded settings.json.
// The pi seed writes a FRESH settings.json carrying just the selection (an
// anonymized home starts empty), so there is nothing to merge into; a later
// substrate applier that lands into an image-staged home may merge instead, but
// the seed's own emitted file is exactly the selection. PURE.
func GenerateSettingsJSON(sel ModelSelection) ([]byte, error) {
	return marshalIndent(sel)
}

// marshalIndent serialises v as tab-indented JSON with a trailing newline,
// matching pi's on-disk style (anon-pi writes JSON.stringify(v, null, '\t')+'\n').
func marshalIndent(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ModelsFilePath and SettingsFilePath are the home-relative paths the pi seed
// writes, matching the target home's `~/.pi/agent/` (the applier lands them under
// the resolved home). They are the same paths the homewrite/anonctl appliers
// already assume for the pi seed.
const (
	ModelsFilePath   = ".pi/agent/models.json"
	SettingsFilePath = ".pi/agent/settings.json"
)

// Options carries the pi seed's already-resolved, non-interactive inputs. The
// interactive parts (which models to import, the default, the guarded apiKey) are
// resolved UPSTREAM (Resolve, in probe.go) and arrive here as plain data, so Plan
// stays pure. It embeds seed.Options for the shared Endpoint field.
type Options struct {
	seed.Options

	// Models is the chosen set of model entries to import (already picked). It may
	// be empty (the degraded fallback: a provider pointed at the endpoint with no
	// pickable model).
	Models []Model

	// DefaultModelID is the chosen default model's id (one of Models' ids, or ""
	// when Models is empty).
	DefaultModelID string

	// APIKey is the provider apiKey to write. It has ALREADY passed the api-key
	// guard upstream (a real key only survives with force); the normal value is
	// apikeyguard.PlaceholderAPIKey. Plan writes it verbatim.
	APIKey string

	// Webveil, when non-nil, wires webveil (anonymized web search) by adding a
	// webveil config.json to the plan (see WebveilOptions). It is RESOLVED upstream
	// (the interactive half's ResolveWebveil): default-on when a SearXNG is detected
	// (or the operator confirms the install default), nil when the operator DISABLES
	// webveil or declines the model-only fallback. A nil Webveil is the explicit
	// model-only pi (no webveil config emitted); a non-nil one is the default wired
	// seed. Plan reads only the resolved socket path from it.
	Webveil *WebveilOptions
}

// WebveilOptions is the pi seed's resolved webveil wiring: the SearXNG socket the
// seeded webveil points at. It is present (non-nil on Options) only when webveil
// is ENABLED; its absence is the explicit model-only fallback. It carries the
// already-resolved socket path (the shared install default, or a detected/
// per-account instance's socket); Plan reads it verbatim to synthesise the
// webveil config.json. It carries no enable flag: presence IS enablement.
type WebveilOptions struct {
	// SocketPath is the SearXNG uWSGI Unix-domain-socket path the seeded webveil
	// baseUrl points at (as `unix:<SocketPath>`). Empty means the install default
	// (DefaultSearxngSocketPath), so a bare enable still yields a valid config.
	SocketPath string
}

// Seed is the pi seed type. It applies to the anonctl substrate today (the
// anonbox substrate is a future task). On the anonctl substrate it emits the
// model files, the model's --allow exception, AND (default-on, disable-able) a
// webveil config.json pointed at a SearXNG over a Unix socket (see webveil.go).
// It CARRIES its already-resolved pi Options (built UPSTREAM by
// Resolve, the interactive half): the seed.Seed interface's Plan takes only the
// minimal seed.Options, but the pi seed's picks (which models, the default, the
// guarded apiKey) do not fit there, so the seam is a resolved-options-bearing
// seed value rather than a field smuggled into the shared seed.Options (which
// would re-mean that deliberately-minimal type). Plan stays PURE: it is a
// deterministic function of the carried, already-resolved Options.
//
// Construct one with New(resolvedOptions); the zero Seed has no picks and Plan
// refuses it (Plan cannot invent the picks).
type Seed struct {
	opts     Options
	resolved bool
}

// New builds a pi Seed carrying already-resolved Options (from Resolve). The CLI
// resolves interactively upstream, then hands the resulting seed.Seed to the
// driver; the driver's generic seed.Options carries only the endpoint, so the
// picks ride on the seed value here.
func New(opts Options) Seed {
	return Seed{opts: opts, resolved: true}
}

// Name is the seed-type name, matching the `anonseed pi` subcommand.
func (Seed) Name() string { return "pi" }

// Targets lists the substrates the pi seed applies to. Today only anonctl (the
// anonbox applier is deferred, a separate task); on anonctl the plan carries the
// model files plus the default-on webveil config.
func (Seed) Targets() []seed.Target { return []seed.Target{seed.TargetAnonctl} }

// Plan synthesises the pi seed's SeedPlan for target: a models.json (one
// provider, openai-completions, endpoint baseUrl, the resolved apiKey), a
// settings.json (defaultProvider/defaultModel/enabledModels), both under
// `~/.pi/agent/`, plus the endpoint as the one `--allow` Exception. It is PURE:
// no filesystem or network I/O. It uses the CARRIED, already-resolved Options; a
// zero Seed (no Resolve run) is rejected, since Plan cannot invent the picks.
//
// The passed seed.Options is honoured for the endpoint when the carried Options
// left it empty (so a caller may still steer the endpoint through the generic
// seam), but the picks always come from the resolved Options.
func (s Seed) Plan(_ context.Context, opts seed.Options, target seed.Target) (seed.SeedPlan, error) {
	if !s.resolved {
		return seed.SeedPlan{}, fmt.Errorf("piseed: Plan needs resolved pi Options (build the seed with piseed.New after piseed.Resolve); got an unresolved seed")
	}
	po := s.opts
	if strings.TrimSpace(po.Endpoint) == "" {
		po.Endpoint = opts.Endpoint
	}
	return po.plan(target)
}

// plan is the pi Options' own pure synthesis, so a caller holding the richer
// Options can synthesise directly without the seed.Seed round-trip.
func (o Options) plan(_ seed.Target) (seed.SeedPlan, error) {
	endpoint := strings.TrimSpace(o.Endpoint)
	if endpoint == "" {
		return seed.SeedPlan{}, fmt.Errorf("piseed: Plan needs a non-empty endpoint")
	}

	modelsJSON, err := GenerateModelsJSON(endpoint, o.Models, o.APIKey)
	if err != nil {
		return seed.SeedPlan{}, fmt.Errorf("piseed: synthesising models.json: %w", err)
	}

	ids := make([]string, 0, len(o.Models))
	for _, m := range o.Models {
		ids = append(ids, m.ID)
	}
	sel := GenerateModelSelection(ids, o.DefaultModelID)
	// When webveil is wired, DECLARE the pi-webveil extension in the seeded
	// settings.json `packages` so pi materialises it on first run. The config file
	// below is not enough on its own: without the extension declared, pi has no
	// webveil to read that config. This is a declarative install (no `pi install`
	// exec). See piseed.WebveilPackage.
	if o.Webveil != nil {
		sel.Packages = append(sel.Packages, WebveilPackage)
	}
	settingsJSON, err := GenerateSettingsJSON(sel)
	if err != nil {
		return seed.SeedPlan{}, fmt.Errorf("piseed: synthesising settings.json: %w", err)
	}

	files := []seed.FileToWrite{
		{Path: ModelsFilePath, Content: string(modelsJSON)},
		{Path: SettingsFilePath, Content: string(settingsJSON)},
	}

	// webveil (default-on, disable-able): when enabled (Webveil non-nil), add the
	// webveil config.json under the XDG subtree (.config/webveil/, NOT under .pi/).
	// It adds NO Exception: a Unix socket has no IP/port, so anonctl needs no
	// `--allow` hole for the SearXNG hop. A nil Webveil is the explicit model-only
	// fallback (no webveil config emitted, and no pi-webveil package declared above).
	if o.Webveil != nil {
		webveilJSON, err := GenerateWebveilConfig(o.Webveil.SocketPath)
		if err != nil {
			return seed.SeedPlan{}, fmt.Errorf("piseed: synthesising webveil config.json: %w", err)
		}
		files = append(files, seed.FileToWrite{Path: WebveilConfigPath, Content: string(webveilJSON)})
	}

	return seed.SeedPlan{
		Files: files,
		Exceptions: []seed.Exception{
			{
				Allow:  hostPortKey(endpoint),
				Reason: "the local model endpoint the seeded pi reaches directly",
			},
		},
	}, nil
}

// Plan is Options' exported pure synthesis: a caller holding resolved Options can
// synthesise the SeedPlan directly (the same result Seed.Plan produces) without
// building a Seed. It is PURE.
func (o Options) Plan(target seed.Target) (seed.SeedPlan, error) { return o.plan(target) }

// ensure Seed satisfies the seed.Seed interface at compile time.
var _ seed.Seed = Seed{}
