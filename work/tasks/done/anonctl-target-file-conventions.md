---
title: The anonctl target — write default-home / account home + defaults.json allow
slug: anonctl-target-file-conventions
prd: anonseed-config-seeder
blockedBy: [seed-interface-and-seedplan, anoncore-seedhome-safe-write, allow-exemption-guardrail]
covers: [8, 11, 12, 13]
---

## What to build

The anonctl `Target`: apply a `SeedPlan` onto anonctl's ON-DISK FILE CONVENTIONS. anonseed WRITES these conventions directly (it imports anoncore only; it does NOT import anonctl — the seam between the two tools is the file layout, deliberately dependency-free):

- **Home seeding** into either the box-wide default-home `/etc/anonctl/default-home/` (the template every fresh `anonctl add` inherits) OR a specific already-provisioned anon account's home — the two sub-targets. Files land via the safe-write surface (`anoncore-seedhome-safe-write`), create-only.
- **Exemption declaration**: merge each `SeedPlan.Exception` into `/etc/anonctl/defaults.json`'s `"allow"` list (config key `"allow"`), each value passing the fail-fast pre-validation (`allow-exemption-guardrail`) first. Preserve any existing `allow` entries (merge, do not clobber the file).

End to end: given a `SeedPlan` (files + exceptions) and a chosen sub-target, the files land in the right home and the exemptions are merged into `defaults.json` — all under a base dir that tests can point at a temp path.

## Acceptance criteria

- [ ] Writes into `/etc/anonctl/default-home/` (box-wide) OR a named account home, selectable; uses the safe-write surface (create-only, anoncore seedhome semantics).
- [ ] Merges exceptions into `/etc/anonctl/defaults.json` `"allow"` (create the file if absent; preserve existing entries; no duplicate entries).
- [ ] The `/etc/anonctl` base path is behind an override (a base-dir seam) so tests never touch the real `/etc/anonctl`.
- [ ] Imports anoncore only; does NOT import anonctl (the integration is the file convention). The `defaults.json` shape matches anonctl's (`{"allow": [...]}`).
- [ ] **Shared-write isolation:** tests point the anonctl base dir at a temp dir and assert the real `/etc/anonctl` is UNTOUCHED.
- [ ] Tests cover: default-home write, named-account write, defaults.json create + merge (existing entries preserved), the base-dir isolation.

## Blocked by

- `seed-interface-and-seedplan` (consumes `SeedPlan`).
- `anoncore-seedhome-safe-write` (the home write surface).
- `allow-exemption-guardrail` (pre-validates each exemption before writing).

## Prompt

> Goal: deliver the anonctl target — the buildable-now substrate that lands a seed plan onto anonctl's on-disk conventions. Domain: anonctl seeds a fresh account's home from the directory-exists `/etc/anonctl/default-home/` template, and applies default LAN exemptions from `/etc/anonctl/defaults.json` (`{"allow": [...]}`). These are DEPENDENCY-FREE file conventions (anonctl documents them as `cp`-able); anonseed writes them directly and imports anoncore ONLY — NEVER anonctl (anonctl's own logic is in its Go-internal `internal/defaults`, un-importable, and that is fine because the contract is the file layout).
>
> Build the anonctl `Target` applier: consume a `SeedPlan` (`seed-interface-and-seedplan`), write `Files` into the chosen home via the safe-write surface (`anoncore-seedhome-safe-write`, create-only), and merge each `Exception` into `defaults.json` `"allow"` after the fail-fast pre-check (`allow-exemption-guardrail`). Two sub-targets: box-wide default-home vs a specific account home. Put the `/etc/anonctl` base path behind an override so tests use a temp dir.
>
> FIRST, check against reality: confirm on disk the real `defaults.json` shape and the `default-home` convention (anonctl `internal/defaults` documents `DefaultBaseDir = /etc/anonctl`, `default-home`, `defaults.json`, key `"allow"`), and the landed shapes of the three blocker tasks. Do not assume; read.
>
> Test with the base dir pointed at a temp path; assert the real `/etc/anonctl` is untouched (shared-write isolation is mandatory here — this writes a system path). Done = both sub-targets seed, defaults.json merges without clobbering, isolation proven, gate green.
