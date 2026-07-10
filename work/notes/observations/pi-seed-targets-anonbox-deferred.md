# 2026-07-10 — pi seed `Targets()` declares anonctl only; the anonbox forward-pointer is deferred, not honoured-in-place

## What happened

`anonbox-target-stub` (PR #7) recorded a forward-pointer in its decisions note: "when `pi-seed-model-config` builds the real pi `Seed`, adding `seed.TargetAnonbox` to its `Targets()` is a one-line declaration into this now-proven mechanism." It expected the pi seed to eventually list anonbox so `--target anonbox` resolves for the pi seed.

`pi-seed-model-config` (PR #9) shipped the pi `Seed` with `Targets() []seed.Target { return []seed.Target{seed.TargetAnonctl} }` — anonctl ONLY. Its decisions note documents the choice: this is the model-config HALF; the anonbox applier is a separate deferred task and the pi seed's webveil behaviour differs by target (anonbox = SearXNG baked into the staged image, story 22b; anonctl = per-account host Unix socket, story 22c), so the model-config half declares anonctl.

## Why this was APPROVED, not blocked (conductor Gate-3 call)

- The `pi-seed-model-config` task body carries NO drift-note / must-fix requiring anonbox in `Targets()`. The anonbox expectation lived only as an observation in a SIBLING task's decisions note (a forward-pointer at observation level), not as a binding criterion of this task.
- All six of `pi-seed-model-config`'s OWN acceptance criteria are met (endpoint-scoped selection, pure Plan, two file shapes + exception, real-key refusal, isolation, no-leak).
- The anonctl-only choice is documented and defensible: declaring anonbox now would let `--target anonbox` route the pi seed to the stub applier (`anonbox.Apply` → `ErrNotYetAvailable`) with an incomplete webveil story. The agent judged the model-config half should not claim anonbox support prematurely.
- The declaration mechanism remains a proven one-liner (the anonbox stub's tests prove a seed CAN declare anonbox and be routed); nothing is lost.

## The open question this leaves (for the target-flag / anonbox-fill-in work)

When the anonbox applier is filled in (and/or `pi-seed-webveil-anonctl-socket` + a future anonbox-webveil task land), whoever owns that work should decide whether the pi seed's `Targets()` gains `seed.TargetAnonbox`. Two coherent positions:

1. **Add anonbox to `Targets()` once the anonbox pi delivery is real** (home+exception seeding is already substrate-agnostic; only image staging + image-webveil are anonbox-specific). This honours the original anonbox forward-pointer.
2. **Keep it anonctl-only until anonbox fully exists**, so `--target anonbox` for the pi seed SKIPS (driver non-fatal skip) rather than routing to a not-yet-available applier — arguably a cleaner UX than a loud not-yet-available for a substrate that literally cannot run.

Both are defensible; the choice is a small design call for the task that makes anonbox real, not a bug in `pi-seed-model-config`. Recorded here so the forward-pointer is not silently dropped.

## Touches

- `target-flag-and-detection` (routes `--target`; will observe the pi seed declares only anonctl today — a `--target anonbox` request for the pi seed will SKIP under the current `Targets()`).
- a future anonbox-applier-fill-in task (owns the decision above).
