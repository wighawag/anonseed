# 2026-07-10 — API-key credential guard: benign-set + divergence from anon-pi

Decisions made while building `apikey-credential-guard` (the load-bearing credential-shedding guard). Recorded here so the done record can link them; each also lives as a comment at its code site in `internal/apikeyguard/apikeyguard.go`.

## The benign set (mirrors anon-pi exactly)

`internal/apikeyguard`'s `benignAPIKeys` mirrors anon-pi's `BENIGN_API_KEYS` VERBATIM (`../anon-pi/packages/anon-pi/src/anon-pi.ts`): the values a genuinely-local model ignores, safe to carry into an anon home. The set is:

    "" , "none", "ollama", "no-key", "nokey", "local", "dummy", "sk-no-key-required"

Membership is tested against the TRIMMED, lower-cased key (same as anon-pi's `apiKeyLooksReal`, which does `apiKey.trim().toLowerCase()`), so " None " and "OLLAMA" are benign. Anything outside the set is treated as a REAL secret. The exported `PlaceholderAPIKey = "none"` mirrors anon-pi's `LOCAL_PROVIDER_API_KEY` (a member of the set, so a seed that writes it is never refused). No values were added or removed; keeping the set identical means anonseed classifies exactly what anon-pi classified, so a key benign under anon-pi stays benign here (and vice versa).

## The one deliberate divergence: no `undefined` case

anon-pi's `apiKeyLooksReal(apiKey: string | undefined)` special-cases `undefined` -> not real (an absent key cannot re-link an identity). Go has no `undefined`; an unset key is the empty string, which is ALREADY in the benign set. So `LooksReal(string)` needs no separate nil/absent branch: the empty string covers both "absent" and "explicitly empty". This is a language-shaped divergence, not a policy one; the classification outcome is identical to anon-pi's (absent or empty -> benign -> passes).

## Shape: `LooksReal` + `Guard` + typed `ErrRealAPIKey`, force is a plain bool

The package exposes a pure classifier `LooksReal(apiKey) bool` and a refuse-unless-forced seam `Guard(apiKey, force) error` returning a typed `*ErrRealAPIKey` (so a caller can `errors.As` it, e.g. to set an exit code or print the flag hint). `force` is a plain bool parameter, NOT a CLI flag: wiring anon-pi's `--force-allow-local-llm-api-key` flag onto this parameter is the pi-seed / CLI layer's job (a separate task, story 21). This package owns ONLY the pure classification + the refusal message, so the invariant holds at the one seam every seed funnels a key through, independent of argv. The refusal message names the risk loudly (an anon identity carrying a real key still authenticates AS the operator, defeating the anonymization) and names the force flag by its anon-pi name so the CLI wiring stays consistent.

Alternatives considered and dropped as premature surface (no consumer yet): a `Refuse(context)` wrapper helper and a package-level `errors.As` convenience. Left out to keep the seam minimal; a future seed can `%w`-wrap `*ErrRealAPIKey` itself.

## Concept-coherence check

"API-key credential guard" is the existing CONTEXT.md glossary term for exactly this (a KEY-VALUE guard anonseed OWNS), explicitly DISTINCT from anoncore seedhome's file-level credential-shedding (setuid/symlink/mode-700, the FILE-BITS guard). The package doc and `homewrite`'s doc both state this boundary, so the two guards are not conflated. No new concept was introduced; `internal/apikeyguard` is the code home for the already-glossed term. It slots in at the point named by ADR 0001 + `seed.SeedPlan`: BEFORE a key enters a plan (the seed calls `Guard` before placing a key into the `models.json` bytes it declares as a `FileToWrite`).

## Acceptance seam (story 25)

`TestSeededHomeNeverContainsRealCredential` asserts the spec's core invariant directly: it seeds a real home (a temp fixture) through the REAL `homewrite.Write` + anoncore `seedhome` (chown behind a fake Runner, no root), then walks every on-disk file and fails if any contains the known real secret or an `apiKey` that `LooksReal`. It also asserts a normal (non-forced) seed with a real key is refused BEFORE any file is written, so the credential never reaches the home in the first place.
