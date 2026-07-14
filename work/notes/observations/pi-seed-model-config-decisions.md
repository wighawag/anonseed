# 2026-07-10 — pi seed (model config): the resolve/plan split, the picks-carrier seam, and the placeholder-key default

Decisions made while building `pi-seed-model-config` (spec `anonseed-config-seeder`, stories 15-21). Recorded here so the done record can link them; each also lives as a doc comment at its choice site in `internal/piseed`.

## Where the resolved picks ride: on the Seed VALUE (New), not smuggled into `seed.Options`

The `seed.Seed` interface's `Plan(ctx, seed.Options, target)` takes only the deliberately-minimal `seed.Options` (just `Endpoint`; its own doc says "Concrete seeds may embed richer, seed-specific option structs around this in their own packages"). The pi seed needs richer resolved inputs (which models, the default, the guarded apiKey) that do NOT fit in `seed.Options`.

Decision: the pi `Seed` CARRIES its already-resolved `piseed.Options`, constructed by `piseed.New(opts)` after the interactive `Resolve`. `Plan` is a pure function of that carried, resolved state (the passed `seed.Options` only supplies the endpoint as a fallback when the carried one is empty). Alternative considered: add an opaque `Extra any` / pi-specific field to the shared `seed.Options`. Dropped: that RE-MEANS the deliberately-minimal shared type (from `seed-interface-and-seedplan`) and would let any seed smuggle arbitrary state through the generic seam, blurring the "Plan is pure over declared inputs" contract. Keeping the picks on the pi package's own value leaves `seed.Options` untouched. Touches: the CLI/driver wiring (task `target-flag-and-detection`) will build the seed via `piseed.New(resolvedOpts)` and hand THAT `seed.Seed` to the driver, rather than passing picks through `seed.Options`.

## The seeded apiKey DEFAULTS to the neutral placeholder, even for a benign-but-nonempty matched key

anon-pi's `generateModelsJson` writes whatever benign key it is handed. Here, `Resolve` normalises a benign-but-nonempty matched key (e.g. `"ollama"`, `"local"`) to `apikeyguard.PlaceholderAPIKey` (`"none"`) for the seeded home. A real key only ever reaches the plan under an explicit `Force` (then it is written verbatim, the operator's auditable override).

Rationale: a genuinely local model ignores its apiKey, and the ANONYMIZED home should carry the neutral value, not the host's chosen placeholder (which is a faint fingerprint of the host's config). This is a USER-VISIBLE default, so it is recorded here rather than buried. Alternative considered: pass the benign key through verbatim (anon-pi's behaviour). Dropped as the less-anonymous default; the guard already treats both as benign, so nothing downstream refuses either. Touches: nothing else (the guard classification is unchanged; only the seeded VALUE for the benign-nonempty case differs).

## The Exception's `Allow` carries the normalised `host:port`; validation is the applier's job

`Plan` emits the endpoint exception as `seed.Exception{Allow: hostPortKey(endpoint)}` (normalised, scheme/path/creds stripped). It does NOT run `allowguard.Parse` — that fail-fast guardrail is the substrate applier's job (task `anonctl-target-file-conventions`), and anonctl re-validates authoritatively at apply time (ADR-0002). This keeps `Plan` pure and declarative (intent only), matching `seed.Exception`'s "declarative carrier" role. Touches: the anonctl applier will `allowguard.Parse` this raw `Allow` before landing it.

## Scope boundary held: no CLI wiring, no webveil, no target detection

This task is the model-config half as a library `Seed` + the interactive `Resolve` seam. The CLI handler (`internal/cli/seed_pi.go` `piStub`) is deliberately NOT replaced: wiring the CLI to the driver is task `target-flag-and-detection` (still in `ready/`), and webveil is task `pi-seed-webveil-anonctl-socket`. Replacing the stub now would entangle this task with those unbuilt dependencies. So `piseed` ships as a usable package (its `Seed`/`Plan`/`Resolve` are exported) that those tasks wire in.
