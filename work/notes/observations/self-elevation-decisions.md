# 2026-07-10 — self-elevation for /etc/anonctl writes: in-scope build decisions

Decisions made while building `self-elevation-for-etc-writes`. Recorded here so the done record can link them; the durable rationale + the acceptance-required divergence record lives in `docs/adr/0003-self-elevation-mirrors-anonctl-with-loud-unavailable.md`, and each decision also has a comment at its code site.

## The elevation seam lives in an importable `internal/elevate`, not `package main`

anonctl's `elevate.go` is private in `package main`. anonseed's root-requiring step is reached deep inside seed handling (not at top-level dispatch), and the applier that writes `/etc/anonctl` is a SEPARATE task (`anonctl-target-file-conventions`). So the decision + argv-construction is a reusable `internal/elevate` package that both the CLI gate and that future applier call, exposing `Ensure(ctx, argv, notice) Decision`. The four impure steps (geteuid / sudo lookup / self path / re-exec) are package-var seams the tests swap. Alternative considered: replicate anonctl's private-in-main shape — rejected because anonseed needs to call the seam from more than one site.

## Gate placed at CLI seed-dispatch (not after --target parsing)

The elevation gate fires in `internal/cli` (`ensureElevated`) when a recognised seed is dispatched, because every built-in seed writes host state under `/etc/anonctl` and so needs root. The finer `--target {anonctl,anonbox}` axis (which would let a non-anonctl or plan-only path skip elevation) is a separate unbuilt task (`target-flag-and-detection`), so gating there now would build on a flag that does not exist. The loop-guard sentinel makes seed-dispatch placement safe (the elevated child re-enters with the sentinel set and proceeds without re-elevating), and moving the gate behind `--target`/a dry-run later touches only the call site, not `elevate`. This TOUCHES the `target-flag-and-detection` and `anonctl-target-file-conventions` tasks (they will consume/relocate this seam), which is why it is recorded rather than buried. Alternative considered: wait for `--target` before wiring any gate — rejected because this task's blocker is only the CLI skeleton, and it is meant to land the seam ahead of the applier.

## Elevation-unavailable is a LOUD failure, diverging from anonctl's fall-through

The load-bearing divergence (full record: ADR-0003, divergence #2). anonctl falls through to a verb's own "must be root" error when sudo is absent; anonseed has no such downstream error at this seam and must never silently proceed unprivileged onto a `/etc/anonctl` write (a partial write of root-owned host state). So `Ensure` returns a non-nil `Err` (wrapping `ErrSudoNotFound`) and the caller exits non-zero BEFORE any write. This introduces a new REFUSAL path, hence recorded as a decision, not a silent factual gap.

## `ANONSEED_ELEVATED` loop-guard sentinel

New env var, the anonseed-namespaced sibling of anonctl's `ANONCTL_ELEVATED` (same loop-guard role, different tool prefix). Checked against CONTEXT.md: no prior anonseed sentinel/concept it could re-mean or overlap. Set on the child before re-exec; if already set on entry, `Ensure` never re-execs.
