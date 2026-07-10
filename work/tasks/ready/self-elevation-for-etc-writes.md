---
title: Self-elevation for root-requiring /etc/anonctl writes
slug: self-elevation-for-etc-writes
prd: anonseed-config-seeder
blockedBy: [bootstrap-go-module-and-cli-skeleton]
covers: [10]
---

## What to build

Self-elevation for the operations that must write under `/etc/anonctl` (root-owned): rather than printing commands for the operator to paste, anonseed re-executes itself with elevated privilege to do the privileged work itself, mirroring anonctl's stance (`../anonctl/elevate.go`). A non-privileged invocation that reaches a root-requiring step should self-elevate (e.g. via `sudo`/re-exec), fail loudly if elevation is unavailable, and never silently degrade.

End to end: a seed operation that needs to write `/etc/anonctl` detects it lacks the privilege and re-execs elevated (or, in tests, the elevation decision + command construction is exercised behind a seam without actually elevating).

## Acceptance criteria

- [ ] A root-requiring path detects insufficient privilege and self-elevates (re-exec), instead of printing a "run this as root" hint.
- [ ] Elevation unavailable = a loud, clear failure (never a silent skip or a partial write).
- [ ] The elevation decision + re-exec command construction is behind a seam so it is unit-testable WITHOUT actually running sudo/root.
- [ ] Matches anonctl's self-elevation stance (reference `../anonctl/elevate.go`); document any deliberate divergence.
- [ ] Tests cover: needs-elevation-and-elevates (via the seam), already-root (no re-exec), elevation-unavailable (loud failure).

## Blocked by

- `bootstrap-go-module-and-cli-skeleton` (needs the module + CLI entry to re-exec).

## Prompt

> Goal: make anonseed DO the privileged work itself (mirroring anonctl), not print paste-these-commands. Domain: anonseed writes host state under `/etc/anonctl` (root-owned) for the anonctl target, so it needs privilege; the family stance (anonctl) is self-elevation, not command-printing.
>
> Reference `../anonctl/elevate.go` for the exact stance and mechanism. Build: on reaching a root-requiring step without privilege, re-exec self elevated; if elevation is impossible, fail LOUD (never silently skip or half-write). Put the elevation decision + command construction behind a seam so tests exercise it without real sudo/root.
>
> FIRST, check against reality: read `../anonctl/elevate.go` for the current mechanism and align; confirm the CLI entry from `bootstrap-go-module-and-cli-skeleton` so the re-exec targets the real binary.
>
> Test the decision seam (needs-elevation, already-root, unavailable). Done = self-elevation wired + loud-on-unavailable + testable seam, gate green. RECORD any divergence from anonctl's mechanism.
