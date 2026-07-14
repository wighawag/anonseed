# pi-seed overwrite / collision UX

## What changed

The `anonseed pi` seed was create-only but had NO way past a collision: the seedhome error said `pass --force to overwrite`, yet no such CLI flag existed (`--force` was undefined; only `--force-allow-local-llm-api-key`, an unrelated api-key concern, existed). So a re-seed of an already-seeded home dead-ended.

Added two escape hatches, both routed through a single `target.OverwritePolicy` seam consulted ONLY on a `seedhome.ErrCollision`:

- `--overwrite` flag: pre-authorises the overwrite with no prompt (for non-interactive re-seeds). Wires `target.AlwaysOverwrite`.
- Interactive prompt (default, no flag): on a collision, lists the colliding home-relative paths and asks `[y/N]`, defaulting to NO (create-only stays the safe default; EOF / non-interactive is also NO). Behind the handler's `overwritePrompt` seam (`overwritePromptFrom` is the testable core).

The applier (`target.AnonctlDefaultHomeApplier`) is now create-only-FIRST with an overwrite fallback: it applies with `force=false`, and on an `*seedhome.ErrCollision` consults the policy, re-applying with `force=true` only when authorised. The collision check is atomic (nothing written on the first attempt), so the retry is safe.

## Naming: `--overwrite`, NOT `--update`

`--update` was considered and REJECTED: anonctl already owns an `add`/`update` pair where `update` means "modify an existing default exemption". Reusing `--update` here would fork that established verb under a new meaning (the REVIEW-PROTOCOL flags exactly this: a new name for an existing concept). `--overwrite` matches the codebase's own create-only/overwrite domain language (`homewrite.go`, spec story 14: "never overwriting an existing seeded file without an explicit force") and reads truthfully against the collision message.

## Deliberately NOT done: a pre-`resolveSeed` pre-flight

The question "should we not check for the default-home first?" was raised. The authoritative collision handling stays at APPLY time (with the interactive prompt), and NO separate pre-flight was added before the model-pick / `resolveSeed` step. Reasons:

- The CLI/handler layer is deliberately kept FREE of the `/etc/anonctl` base path and the home-path knowledge (stated in multiple package docs: `homewrite`, `anonctl`, `target.DefaultAppliers`). A pre-flight before the plan exists would have to hardcode or re-derive the target home + the seed's file paths, duplicating that layering.
- It would race the atomic collision check (state can change between pre-flight and apply), so the apply-time check must stay authoritative anyway.
- The interactive prompt at apply already gives the operator an early, actionable decision at the right moment (the model pick happened during `resolveSeed`, but the prompt now replaces the old dead-end error). The residual cost is only that the model-pick effort precedes the overwrite question.

If a pre-flight is later wanted (e.g. to warn before the model pick), the clean way is to thread the target home + expected paths through a dedicated seam rather than reaching into the base path from the handler.
