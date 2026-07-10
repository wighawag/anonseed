---
title: The pi seed — model config (probe, pick, synthesize models.json + settings.json)
slug: pi-seed-model-config
prd: anonseed-config-seeder
blockedBy: [seed-interface-and-seedplan, apikey-credential-guard]
covers: [15, 16, 17, 18, 19, 20, 21]
---

## What to build

The pi seed's model half (mirrors anon-pi's `init` model-config), as a `Seed` implementation whose `Plan` produces the model files + the model's `--allow` exception. The flow:

- INTERACTIVE step (UPSTREAM of `Plan`, kept separate so `Plan` stays pure): take the local model endpoint's `host:port`, probe its live `/v1/models`, AND read the provider in the user's own `~/.pi/agent/models.json` whose baseUrl matches that endpoint; let the user PICK which models to import and which is the default.
- PURE `Plan`: synthesize a `models.json` carrying exactly ONE provider (pointed at the endpoint, api `openai-completions`, baseUrl the LAN/loopback endpoint, apiKey a PLACEHOLDER) and a `settings.json` (defaultProvider / defaultModel / enabledModels), as `SeedPlan.Files` under the home's `~/.pi/agent/`; plus the endpoint's `host:port` as a `SeedPlan.Exception` (the `--allow` hole).
- Consider ONLY the provider served by the matched endpoint (never any other provider/key from the user's config) — anonymity-critical scoping, by construction.
- Before a matched provider's apiKey can enter the plan, run the api-key guard (`apikey-credential-guard`): REFUSE if it looks real, unless forced (a local model ignores its key, so a placeholder is fine).

End to end: given an endpoint (a fake/stub server + a fixture `models.json` in tests) and a pick, the seed yields the two files + the exception; a real matched apiKey refuses unless forced; no non-matched provider leaks in.

## Acceptance criteria

- [ ] Probes `/v1/models` and reads the endpoint-matched provider from the user's `models.json`; ONLY that provider is considered (test with a multi-provider fixture — no other provider/key enters the plan).
- [ ] Interactive pick of models + default is SEPARATE from the pure `Plan` synthesis.
- [ ] `Plan` emits a `models.json` (one provider, `openai-completions`, endpoint baseUrl, PLACEHOLDER apiKey) + a `settings.json` (defaultProvider/defaultModel/enabledModels) under `~/.pi/agent/`, and the endpoint as an `Exception`.
- [ ] A real-looking matched apiKey is REFUSED (via the api-key guard) unless forced; a benign/placeholder one passes.
- [ ] **Shared-write isolation:** the user `models.json` read + the endpoint probe are behind seams (fixture file + fake HTTP); tests write only to temp fixtures.
- [ ] Tests cover: endpoint-scoped provider selection, the two generated file shapes, the placeholder-key output, real-key refusal, no-leak of other providers.

## Blocked by

- `seed-interface-and-seedplan` (implements `Seed`, emits `SeedPlan`).
- `apikey-credential-guard` (gates the matched apiKey before it enters the plan).

## Prompt

> Goal: build the pi seed's model-config half (the FULL v1 scope, mirroring anon-pi's `init`). Domain: given a local model endpoint `host:port`, the pi seed probes the endpoint's live `/v1/models` AND reads the provider in the user's own `~/.pi/agent/models.json` whose baseUrl matches that endpoint, lets the user pick models + default, then generates a pi `models.json` (ONE provider, api `openai-completions`, baseUrl the endpoint, apiKey a PLACEHOLDER) + `settings.json` into the target home's `~/.pi/agent/`, and declares the endpoint as the `--allow` exception. Only the matched provider is read — so no other provider or key can enter the seed BY CONSTRUCTION (anonymity-critical).
>
> Reference `../anon-pi/packages/anon-pi/src/anon-pi.ts`: `findEndpointProvider` (the endpoint-matched scoping), the `models.json` synthesis, `LOCAL_PROVIDER_*` constants (`openai-completions`, benign `none` key), and the models import/pick. Split the flow: the INTERACTIVE probe+pick is UPSTREAM; `Plan` is PURE (deterministic synthesis). Before a matched apiKey enters the plan, call the api-key guard (`apikey-credential-guard`) — refuse a real key unless forced.
>
> FIRST, check against reality: confirm the `Seed`/`SeedPlan` shape from `seed-interface-and-seedplan` and the guard API from `apikey-credential-guard` on disk; confirm the pi `models.json`/`settings.json` shapes against anon-pi.
>
> Test with a fake `/v1/models` server + a fixture user `models.json` (multi-provider, to prove scoping); write only to temp. Done = the two files + exception emitted, scoping proven, real-key refused, gate green.
