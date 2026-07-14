---
title: The --target substrate flag + interactive detect-then-ask + multi-target fan-out
slug: target-flag-and-detection
spec: anonseed-config-seeder
blockedBy: [anonctl-target-file-conventions, seed-interface-and-seedplan]
covers: [22f, 22g]
---

## What to build

The `--target {anonctl,anonbox}` selection surface on `anonseed <seed>`, wiring the CLI to the driver. Behaviour:

- Explicit `--target anonctl` (or `anonbox`) picks the substrate.
- No `--target` = INTERACTIVE default: detect which substrates are PRESENT, then ASK the operator (not a silent auto-pick). May seed AS MANY APPLICABLE targets as are present.
- The fan-out is BOUNDED by the seed's `Targets()`: a seed that does not declare a present substrate is SKIPPED for it (not mis-seeded, not an error).

End to end: `anonseed pi --target anonctl` runs the pi seed's anonctl plan through the anonctl applier; bare `anonseed pi` detects + prompts; a target the seed doesn't support is cleanly skipped.

## Acceptance criteria

- [ ] `--target {anonctl,anonbox}` selects the substrate; an unknown target value fails loudly.
- [ ] No `--target` -> detect present substrates, then prompt (interactive); never silent auto-pick.
- [ ] Multi-target: when several applicable+present targets exist, can seed all of them (bounded by the seed's `Targets()`).
- [ ] A requested/present target the seed does not declare in `Targets()` is skipped cleanly (clear message, not an error).
- [ ] Substrate DETECTION (is anonctl present? is anonbox present?) is behind a seam so tests drive present/absent without the real environment.
- [ ] Tests cover: explicit target, detect-then-ask default, multi-target fan-out, unsupported-target skip.

## Blocked by

- `anonctl-target-file-conventions` (the anonctl applier the flag routes into).
- `seed-interface-and-seedplan` (the `Targets()` contract + driver).

## Prompt

> Goal: give `anonseed <seed>` its `--target` substrate axis. Domain: seed-type and target are ORTHOGONAL — one command per seed (`anonseed pi`), substrate chosen via `--target {anonctl,anonbox}`. Default (no flag) = interactively DETECT which substrates are present and ASK (never silent auto-pick); may seed as many APPLICABLE targets as present, bounded by the seed's `Targets()` (a seed need not support both — skip it for a substrate it does not declare).
>
> Build the flag + detection + the driver wiring: route the chosen target(s) through the driver (`seed-interface-and-seedplan`) into the substrate applier (`anonctl-target-file-conventions` for anonctl; `anonbox-target-stub` for anonbox). Put substrate detection behind a seam so tests set present/absent deterministically.
>
> FIRST, check against reality: confirm the driver + `Targets()` from `seed-interface-and-seedplan` and the anonctl applier entry from `anonctl-target-file-conventions` on disk.
>
> Test explicit/default/multi/skip paths with the detection seam faked. Done = `--target` + detect-then-ask + fan-out + skip, gate green.
