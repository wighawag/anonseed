---
title: The anonbox target as a declared-but-stubbed substrate
slug: anonbox-target-stub
spec: anonseed-config-seeder
blockedBy: [seed-interface-and-seedplan]
covers: [23, 22b]
---

## What to build

The anonbox `Target` as a first-class-but-STUBBED substrate, so the interface is exercised and the `--target anonbox` path resolves, without blocking on anonbox (which does not exist yet). The stub:

- Is a real `Target` value the driver can route to and that seeds declare in `Targets()` where applicable (the pi seed declares it).
- On apply, it does the SAME home / exception seeding shape as the anonctl target would AND is designed to additionally provide/stage the container image that has the tool installed — but since anonbox does not exist, the image-staging + any anonbox-runtime interaction is a clear NO-OP / not-yet-available notice (loud, honest: "anonbox target not yet available"), NOT a silent success and NOT a crash.
- Records the intended shape (home+exception seeding PLUS image staging) so the real implementation later fills the stub.

This keeps the target axis honest (anonbox is a known, declared target) while the real image-based delivery is a deliberate, flagged non-delivery.

## Acceptance criteria

- [ ] An `anonbox` `Target` exists; the driver routes to it and the pi seed lists it in `Targets()` (so `--target anonbox` resolves rather than erroring "unknown target").
- [ ] Applying to anonbox surfaces a clear NOT-YET-AVAILABLE outcome (loud, honest) rather than a silent no-op-success or a crash.
- [ ] The intended anonbox delivery (same home/exception seeding PLUS image staging) is documented at the stub so the later implementation knows the shape.
- [ ] webveil-for-anonbox (story 22b: SearXNG baked into the staged image) is noted as the intended anonbox behaviour at the stub (not implemented, just recorded).
- [ ] Tests cover: the target resolves + routes, and applying yields the not-yet-available outcome.

## Blocked by

- `seed-interface-and-seedplan` (the `Target` + driver contract the stub plugs into).

## Prompt

> Goal: make anonbox a DECLARED target with a STUB applier, so the interface + `--target anonbox` are exercised without blocking on anonbox (which does not exist yet). Domain: the anonbox target does the same home/exception seeding as anonctl AND additionally stages the container image that has the tool installed (the netcage/anonbox image substrate) — for webveil this means SearXNG is baked into the image and already running (the anon-pi/netcage model, proven), so nothing is installed on the host.
>
> Build a real `anonbox` `Target` the driver routes to and seeds can declare in `Targets()`, but whose apply is a LOUD not-yet-available outcome (not a silent success, not a crash). Document the intended real shape at the stub (home+exception seeding + image staging; webveil-via-image per story 22b). This is a deliberate flagged non-delivery, keeping the target axis honest.
>
> FIRST, check against reality: confirm the `Target`/driver contract from `seed-interface-and-seedplan` on disk and plug the stub into it.
>
> Test that the target resolves/routes and apply yields the not-yet-available outcome. Done = anonbox declared + stubbed loudly + intended shape recorded, gate green.
