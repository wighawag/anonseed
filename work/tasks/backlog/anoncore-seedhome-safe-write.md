---
title: Resolve target identity + safe home-write via anoncore seedhome
slug: anoncore-seedhome-safe-write
prd: anonseed-config-seeder
blockedBy: [bootstrap-go-module-and-cli-skeleton]
covers: [6, 7, 14]
---

## What to build

The shared seeding surface below every seed: (1) resolve the target identity into a home path + account, and (2) safely write a set of files into that home. Import anoncore (do NOT vendor, do NOT re-implement):

- `github.com/wighawag/anoncore/account` for the account-name vocabulary (`ResolveAccount`, the `anon` / `anon-<name>` forms) to resolve which home to write into.
- `github.com/wighawag/anoncore/seedhome` (`seedhome.Seed(ctx, runner, templateDir, home, account, force)`) for the credential-shedding write: it strips setuid/setgid/sticky bits, refuses symlinks, writes mode-700, and treats a collision as a LOUD error unless `force` (atomic collision check — writes nothing if any collision).

Expose an anonseed-side function that takes a resolved home + a set of files-to-write (the `SeedPlan.Files`) and lands them through `seedhome`, create-only by default (never overwriting an existing file without an explicit force), consistent with anonctl's create-only `add`.

End to end: given a target home (a temp dir in tests) and a set of files, the files land with the safe semantics; a collision without force fails loudly and writes nothing; setuid bits are stripped.

## Acceptance criteria

- [ ] anonseed imports `anoncore/account` + `anoncore/seedhome`; it does NOT vendor or re-implement the safe-write.
- [ ] Files land into the resolved home via `seedhome.Seed` with its safety semantics (setuid/setgid/sticky stripped, symlinks refused, mode-700).
- [ ] Create-only by default: a collision with an existing file is a loud error unless an explicit force is passed; on collision NOTHING is written (atomic).
- [ ] Target-identity resolution covers BOTH sub-targets: the box-wide default-home path and a specific named account's home.
- [ ] **Shared-write isolation:** tests point the home at a temp/scratch dir and assert no real home/`/etc` path is touched; the real anoncore chown step is behind the Runner seam (use a fake Runner, no root).
- [ ] Tests cover: successful write, collision-refusal (nothing written), setuid-strip, both sub-target resolutions.

## Blocked by

- `bootstrap-go-module-and-cli-skeleton` (needs the module + package layout).

## Prompt

> Goal: wire anoncore's shared safe-home-write so anonseed never re-implements the file-level credential-shedding (it CANNOT drift from anonctl because it is the SAME code). Domain: seeding writes a tool's config FILES into a target identity's home; two sub-targets exist — the anonctl box-wide default-home, and a specific already-provisioned anon account's home.
>
> Use anoncore v0.1.0 (already a family dependency; anonctl imports it): `account.ResolveAccount` for the account vocabulary and `seedhome.Seed(ctx, runner, templateDir, home, account, force)` for the write. Verify the real signature in the module before coding (it is `func Seed(ctx, r Runner, templateDir, home, account string, force bool) (Result, error)`); `seedhome` strips setuid/setgid/sticky, refuses symlinks, writes mode-700, and does an ATOMIC collision check (writes nothing if any collision, unless force). The chown is behind a `Runner` seam — inject a fake in tests so no root/real chown is needed. NOTE: `seedhome` is the FILE-level guard; the API-KEY guard (refuse a real model key) is a SEPARATE task (`apikey-credential-guard`), do not conflate them here.
>
> FIRST, check against reality: confirm the `SeedPlan.Files` shape from `seed-interface-and-seedplan` (this task consumes it) and the anoncore `seedhome.Seed` signature on disk before building.
>
> Test at the home-write seam against a temp dir; assert the real filesystem outside the fixture is untouched. Done = files land safely, collisions refuse atomically, both sub-targets resolve, gate green.
