# 2026-07-10 — anonbox target stub: applier shape, loud-outcome, and the "pi seed declares it" scope decision

Decisions made while building `anonbox-target-stub` (spec `anonseed-config-seeder`, stories 23 + 22b). Recorded here so the done record can link them; the intended anonbox delivery shape also lives verbatim in the `internal/anonbox` package doc.

## The applier is a concrete `Apply(ctx, SeedPlan) error`, not a new shared `Applier` interface

On disk there is NO shared `Applier` interface yet: the "applier" is referenced in comments (`internal/seed/driver.go`, `internal/homewrite`) as "separate tasks" that consume a `SeedPlan` downstream of the driver, and the anonctl applier (task `anonctl-target-file-conventions`) is still in `ready/` (not landed). So there was no applier contract to plug into.

Decision: build the anonbox applier as a concrete package `internal/anonbox` exposing `Apply(ctx context.Context, plan seed.SeedPlan) error`, NOT a new cross-substrate `Applier` interface. Rationale: inventing a shared interface now would front-run the anonctl applier task and risk a concept the two appliers then have to reconcile (a coherence hazard). Each applier stays a plain per-substrate entry point downstream of the driver's skip decision, matching how `target-flag-and-detection` describes routing ("route ... into the substrate applier (`anonctl-target-file-conventions` for anonctl; `anonbox-target-stub` for anonbox)"). If a shared interface is later wanted, both concrete `Apply` functions already share the same `(ctx, SeedPlan) error` signature, so extracting one is mechanical. Touches: `target-flag-and-detection` (routes into this `Apply`), and `anonctl-target-file-conventions` (the sibling applier, free to pick its own signature or align on this one).

## The not-yet-available outcome is a matchable ERROR (`ErrNotYetAvailable`), not a bool/skip

Applying to anonbox returns an error wrapping the sentinel `anonbox.ErrNotYetAvailable` (matchable via `errors.Is`), with NO I/O on the way there. Rationale: the task demands "loud, honest, not a silent success and not a crash". An error value is the loud-and-matchable choice; the side-effect-free body rules out both a partial write and a crash.

Concept-coherence check against the existing "skip": the target axis already has a precise "skip" (a seed whose `Targets()` omits a substrate is SKIPPED by `seed.Run`, a non-fatal non-mis-seed). That is DIFFERENT from "this substrate is not implemented yet": a seed that DOES declare anonbox and is routed there still cannot be delivered. Reusing "skip" would conflate "does not apply here" with "not built yet". So not-yet-available is a distinct ERROR value sitting DOWNSTREAM of the driver's skip, not a second meaning of skip. Documented at the code site (`internal/anonbox` package doc, "Why an error, not a bool or a silent skip").

## The "pi seed lists anonbox in Targets()" acceptance: mechanism proven, no pi Seed exists yet to carry it

The acceptance criterion "the pi seed lists it in `Targets()`" cannot be LITERALLY satisfied by this task, because there is no pi `Seed` on disk yet: `internal/cli/seed_pi.go` is `piStub`, a CLI `Handler` (argv dispatch), NOT a `seed.Seed` (the `Name()/Targets()/Plan()` contract). The pi `Seed` is a SEPARATE ready task, `pi-seed-model-config` (covers 15-21), and it is the natural owner of the pi seed's `Targets()` declaration.

Decision: do NOT fabricate a premature pi `Seed` here just to have something declare `Targets()` (that would front-run `pi-seed-model-config` and likely conflict with it). Instead:

- The DECLARATION MECHANISM is already live and is proven by this task's tests: `seed.TargetAnonbox` exists, a seed can list it in `Targets()`, and the driver ROUTES (does not skip) such a seed to the anonbox applier (`TestSeedCanDeclareAnonboxTarget`, `TestDriverRoutesToAnonboxApplier` in `internal/anonbox/anonbox_test.go`, using an in-test seed that declares anonbox).
- When `pi-seed-model-config` builds the real pi `Seed`, adding `seed.TargetAnonbox` to its `Targets()` is a one-line declaration into this now-proven mechanism.

This is a PROCEED-with-recorded-decision (not a STOP): the task's core — anonbox declared as a target + a loud stub applier + the intended shape recorded + tests for resolve/route/not-yet-available — is fully delivered against reality; only the pi-seed-side declaration is deferred to the task that owns the pi `Seed`. Alternative considered: STOP and route to needs-attention. Dropped: the core is buildable and unambiguous, and the deferred piece is a downstream integration the blocking-graph already assigns elsewhere, not a false premise in THIS task. Touches: `pi-seed-model-config` (will add anonbox to the pi seed's `Targets()`), `target-flag-and-detection` (routes `--target anonbox` into `anonbox.Apply`).

## webveil-for-anonbox (story 22b) is RECORDED, not implemented

On the anonbox target, webveil/SearXNG comes FROM THE STAGED IMAGE (baked in, already running, in-image socket, no `--allow` hole), the proven anon-pi/netcage model — DISTINCT from the anonctl target's host per-account Unix socket (story 22c, task `pi-seed-webveil-anonctl-socket`). This task RECORDS that shape at the stub (`internal/anonbox` package doc, section "webveil for anonbox comes FROM THE IMAGE") so the later image-staging implementation wires webveil from the image; it is not implemented here.
