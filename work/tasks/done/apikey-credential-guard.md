---
title: The API-key credential-shedding guard (refuse a real key unless forced)
slug: apikey-credential-guard
spec: anonseed-config-seeder
blockedBy: [bootstrap-go-module-and-cli-skeleton]
covers: [9, 25]
---

## What to build

The load-bearing safety seam anonseed exists for: a check that a credential (an API key / token) about to be seeded into an anonymized home is NOT a real-looking secret, refusing loudly unless an explicit force flag is passed. Mirror anon-pi's `apiKeyLooksReal`: a small benign-set (placeholder values a genuinely-local model ignores, e.g. `none`, `local`, empty) is allowed; anything outside it is treated as a REAL secret and refused (because an anonymized identity carrying the operator's real credential still authenticates AS the operator, defeating the anonymization).

This is anonseed-owned and DISTINCT from anoncore/seedhome's file-level credential-shedding (setuid/symlink/mode-700). Expose it as a reusable guard the seeds call before a key enters a `SeedPlan`, plus the general acceptance-level assertion "a seeded home never contains a real credential."

End to end: given a candidate apiKey, the guard classifies benign-vs-real; a real key is refused with a clear message unless force; a benign/placeholder key passes.

## Acceptance criteria

- [ ] A pure `apiKeyLooksReal`-style classifier: benign-set (placeholders a local model ignores) passes; everything else is "real".
- [ ] The guard REFUSES loudly (clear message naming the risk) when a real-looking key would be seeded, unless an explicit force flag is set.
- [ ] The benign set + the refuse-unless-forced behaviour mirror anon-pi's (same spirit/values); document any deliberate divergence.
- [ ] An acceptance test asserts the invariant directly: a seeded home (temp fixture) NEVER contains a real-looking credential after a normal (non-forced) seed.
- [ ] Tests cover: benign passes, real refused, forced-real allowed, the seeded-home-has-no-real-credential assertion.

## Blocked by

- `bootstrap-go-module-and-cli-skeleton` (needs the module + package layout).

## Prompt

> Goal: build the credential-shedding guard that is the WHOLE POINT of anonseed's safety story. Domain: an anonymized identity that carries the operator's real API key still authenticates AS the operator — it stops the IP leak while staying identity-linked, defeating anonymity. So anonseed must NEVER seed a real-looking credential into an anon home, and must refuse LOUDLY unless explicitly forced. A genuinely local model ignores its apiKey, so a placeholder is fine; a real one is refused.
>
> Reference: anon-pi already implements this (`apiKeyLooksReal`, the benign set, `--force-allow-local-llm-api-key`) in `../anon-pi/packages/anon-pi/src/anon-pi.ts` — mirror its benign-set + refuse-unless-force. This guard is anonseed-OWNED and DISTINCT from anoncore/seedhome's file-level guard (setuid/symlink/mode-700) — do not conflate; this one is about the KEY VALUE, not file bits.
>
> FIRST, check against reality: confirm the `SeedPlan`/seed shapes from `seed-interface-and-seedplan` so the guard slots in where a key would enter a plan.
>
> Test the classifier (benign vs real, forced) AND write the acceptance-level assertion the spec names as a core seam: a seeded home (temp fixture) never contains a real credential. Done = classifier + refusal + the seeded-home assertion, gate green. RECORD the benign-set choice (and any divergence from anon-pi's) in the done record.
