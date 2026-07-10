---
title: The target-aware declarative Seed interface + SeedPlan types
slug: seed-interface-and-seedplan
prd: anonseed-config-seeder
blockedBy: [bootstrap-go-module-and-cli-skeleton]
covers: [3, 22g]
---

## What to build

The internal contract every built-in seed implements, plus the shared driver that runs a seed against a target. Define:

- `Seed` interface: `Name() string`, `Targets() []Target` (which substrates this seed applies to), and `Plan(ctx, opts, target) (SeedPlan, error)` — a PURE synthesis (no I/O, no interactivity) for a GIVEN target.
- `SeedPlan{ Files []FileToWrite; Exceptions []Exception }` — JSON-serializable (so the future PATH-plugin can emit it on stdout). `Files` are home-relative path + content; `Exceptions` is a LIST (may be empty for a socket-wired service, may hold more than one).
- A `Target` type (`anonctl`, `anonbox`) and the driver seam that: takes a seed + a chosen target, calls `Plan`, and hands the resulting `SeedPlan` to the substrate applier (the substrate appliers themselves are separate tasks). If a seed does not list a requested target in `Targets()`, it is SKIPPED for that target (not mis-seeded).

End to end: a trivial fake seed (returns a fixed `SeedPlan`, supports one target) proves the interface, the `Targets()` skip logic, and JSON round-trip of `SeedPlan`.

## Acceptance criteria

- [ ] `Seed`, `SeedPlan`, `FileToWrite`, `Exception`, `Target` types exist with the shapes above.
- [ ] `Plan` takes the target as an input (a seed's plan may differ by substrate); it is pure (deterministic, no filesystem/network).
- [ ] `SeedPlan` marshals to and from JSON losslessly (test the round-trip).
- [ ] The driver skips a seed for a target the seed does not declare in `Targets()`, with a clear, non-fatal outcome (not an error, not a silent mis-seed).
- [ ] A fake in-test seed exercises the whole seam without touching disk or network.
- [ ] **Config-seeding-ONLY invariant (spec story 2):** the driver + `SeedPlan` express ONLY files-to-write + exceptions-to-declare — there is NO affordance to provision an account or launch/start a runtime or service. Assert this at the type level (a `SeedPlan` cannot carry a "run this" instruction) so a later seed cannot drift into launching. anonseed seeds config; it never provisions or launches.
- [ ] Tests cover: Plan purity, Targets() skip, JSON round-trip. Standard Go test style.

## Blocked by

- `bootstrap-go-module-and-cli-skeleton` (needs the module + package layout).

## Prompt

> Goal: pin the built-in-seed interface (the hard-to-reverse core abstraction) so `pi` and future seeds share one declarative shape. Domain: a SEED declares config FILES to write into a target identity's home plus the direct-egress `--allow` EXCEPTIONS the tool needs; a TARGET is the substrate that delivers the plan (`anonctl` now, `anonbox` later). Seed-type and target are ORTHOGONAL axes.
>
> The pinned shape (from the prd's resolved fork on the seed interface): `Seed{ Name(); Targets() []Target; Plan(ctx, opts, target) (SeedPlan, error) }` and `SeedPlan{ Files []FileToWrite; Exceptions []Exception }`. Three load-bearing decisions to honour: (i) `Exceptions` is a LIST (supports >1 and ZERO — a socket-wired service needs none); (ii) the interface is STRICTLY DECLARATIVE (files + exceptions only — no "launch a service", that lifecycle is anonctl/anoncore's job); (iii) `Plan` is PURE and takes the target as input (the interactive model-pick and the substrate writing live UPSTREAM/DOWNSTREAM of Plan, not inside it). `SeedPlan` MUST be JSON-serializable (the future PATH-plugin emits it on stdout).
>
> FIRST, check against reality: confirm the CLI skeleton from `bootstrap-go-module-and-cli-skeleton` landed with the dispatch registry this interface plugs into; build on its real shape, not an assumed one.
>
> Test at the interface seam with a fake seed (no real substrate). Done = the types + driver exist, the fake seed round-trips and skip-logic works, gate green. RECORD the interface decision (it is the hard-to-reverse core) as an ADR in `docs/adr/` per `ADR-FORMAT.md`.
