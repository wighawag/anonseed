// Package apikeyguard is anonseed's API-key credential-shedding guard: the
// load-bearing safety seam anonseed exists for. It classifies a candidate apiKey
// as benign (a placeholder a genuinely-local model ignores) or REAL (a secret),
// and REFUSES to let a real-looking key be seeded into an anonymized home unless
// an explicit force flag is passed.
//
// Why this matters (the whole point of anonseed's safety story): an anonymized
// identity that carries the operator's real API key still authenticates AS the
// operator. It stops the IP leak while staying identity-linked, defeating the
// anonymization. A genuinely local model ignores its apiKey, so a placeholder is
// fine; a real one is refused loudly.
//
// This guard is anonseed-OWNED and is about the KEY VALUE. It is DISTINCT from
// anoncore seedhome's file-level credential-shedding (setuid/setgid/sticky strip,
// symlink refusal, mode-700), which is about file BITS. The two do not overlap:
// homewrite.Write enforces the file-level guard; this package enforces the
// key-value guard, called BEFORE a key enters a SeedPlan.
//
// It mirrors the sibling tool anon-pi's `apiKeyLooksReal` + benign set +
// refuse-unless-`--force-allow-local-llm-api-key` behaviour (anon-pi, a
// pi-specific launcher, is being retired; its provisioning knowledge becomes
// anonseed's). See the Decisions note in the done record for the benign-set
// values and the one deliberate divergence (Go has no `undefined`, so an
// unset/empty key is the benign empty string, not a separate case).
package apikeyguard

import "strings"

// PlaceholderAPIKey is the benign, non-secret apiKey a seed uses for a local
// provider (a LAN/loopback model rarely needs a real key). It is a member of the
// benign set, so a seed that writes this value is never refused. Mirrors
// anon-pi's LOCAL_PROVIDER_API_KEY.
const PlaceholderAPIKey = "none"

// benignAPIKeys is the set of apiKey values that are NOT real secrets and are
// therefore safe to carry into an anonymized seed verbatim. Membership is tested
// case-insensitively against the TRIMMED key (see LooksReal). Anything outside
// this set is treated as a REAL secret. These values mirror anon-pi's
// BENIGN_API_KEYS exactly (placeholders a genuinely-local model ignores).
var benignAPIKeys = map[string]struct{}{
	"":                   {},
	"none":               {},
	"ollama":             {},
	"no-key":             {},
	"nokey":              {},
	"local":              {},
	"dummy":              {},
	"sk-no-key-required": {},
}

// LooksReal reports whether apiKey looks like a REAL secret, i.e. is NOT in the
// benign set of placeholder values a genuinely-local model ignores. It is PURE:
// no I/O, deterministic, and the sole classifier the guard's refusal decision
// rests on.
//
// The key is compared TRIMMED and case-insensitively (matching anon-pi), so
// " None " and "NONE" are benign. An empty (or whitespace-only) key is benign:
// it is the anon-pi `undefined`/empty case, an absent credential, which cannot
// re-link an identity.
func LooksReal(apiKey string) bool {
	_, benign := benignAPIKeys[strings.ToLower(strings.TrimSpace(apiKey))]
	return !benign
}

// ErrRealAPIKey is the typed refusal a guard raises when a real-looking apiKey
// would be seeded without force. Callers can errors.As it to distinguish this
// load-bearing safety refusal from other failures (e.g. to set a specific exit
// code, or to print the force-flag hint). Its Error() names the risk explicitly.
type ErrRealAPIKey struct{}

func (ErrRealAPIKey) Error() string {
	return "the candidate apiKey looks like a REAL secret. Seeding it would put a " +
		"host credential into the anonymized home, so the anonymized identity would " +
		"still authenticate AS the operator (it stops the IP leak while staying " +
		"identity-linked, defeating the anonymization). Refusing. If this key is " +
		"genuinely safe for a local model, re-run with the force flag " +
		"(--force-allow-local-llm-api-key) to carry it through"
}

// Guard is the refuse-unless-forced seam every seed calls BEFORE a key enters a
// SeedPlan. It classifies apiKey via LooksReal:
//
//   - a benign/placeholder key always passes (returns nil);
//   - a real-looking key is REFUSED with an *ErrRealAPIKey (a clear message
//     naming the risk) UNLESS force is true;
//   - a real-looking key with force == true passes (returns nil), the operator's
//     explicit, auditable override.
//
// force corresponds to anon-pi's `--force-allow-local-llm-api-key`. Wiring that
// CLI flag to this parameter is the seed/CLI layer's job (a separate task); this
// package only owns the pure classification + the refusal, so the invariant "a
// seeded home never contains a real credential" holds at the one seam every seed
// funnels a key through.
func Guard(apiKey string, force bool) error {
	if !LooksReal(apiKey) {
		return nil
	}
	if force {
		return nil
	}
	return &ErrRealAPIKey{}
}

// ensure the sentinel type's pointer satisfies error (so callers can errors.As
// it and %w-wrap it cleanly).
var _ error = (*ErrRealAPIKey)(nil)
