# The target-aware, strictly-declarative Seed interface

Every built-in seed implements `Seed{ Name() string; Targets() []Target; Plan(ctx, opts, target) (SeedPlan, error) }`, producing a `SeedPlan{ Files []FileToWrite; Exceptions []Exception }`. Seed-type and target are orthogonal axes: a seed declares which substrates it applies to via `Targets()`, and the driver skips a seed for any target it does not declare (a non-fatal skip, never a mis-seed). We pin this now because it is the hard-to-reverse core abstraction every future seed and the reserved PATH-plugin depend on.

## Considered Options

- **Per-target seed interfaces** (a `Seed` per substrate). Rejected: it couples seed-type to substrate, the two axes are orthogonal, and it would force a seed author to know about substrates they do not care about.
- **`Plan` performing its own I/O / interactivity** (probing endpoints, prompting for model choice inside `Plan`). Rejected: it makes `Plan` untestable and non-deterministic. Instead `Plan` is PURE — the interactive model-pick is resolved UPSTREAM and arrives as plain `Options` data, and the substrate writing lives DOWNSTREAM in separate appliers.
- **A richer `SeedPlan` that could also express "launch this service" / "provision this account".** Rejected deliberately and load-bearingly (see below).

## Consequences

- **Strictly declarative — config-seeding ONLY.** `SeedPlan` can express only files-to-write and direct-egress exceptions. It carries NO affordance to launch a runtime/service or provision an account: those lifecycles belong to anonctl/anonbox/anoncore, not anonseed. A type-level test (`TestSeedPlanIsConfigSeedingOnly`) enforces that the declarative surface exposes only the allowed fields and no run/exec/launch/provision-flavoured field, so a later seed cannot quietly drift into launching.
- **`Exceptions` is a LIST.** It supports zero (a Unix-socket-wired service needs no `--allow` hole) and more than one, rather than a single optional exception.
- **`SeedPlan` is JSON-serializable** (round-trips losslessly), so the reserved PATH-plugin escape hatch's contract can simply be "emit a `SeedPlan` on stdout" without reopening this shape.
- **The driver is the seam** between "which seed for which target" and "apply this plan"; the substrate appliers (write files via anoncore `seedhome`, declare the `--allow` exceptions) are separate tasks that consume a `SeedPlan` and never see the seed.
- This `Seed` interface is distinct from and sits above the CLI's argv-dispatch `Handler` (in `internal/cli`): `Handler` routes a subcommand's argv, `Seed` declares a plan. Keeping them separate layers keeps the pure planning contract free of argv/exit-code concerns.
