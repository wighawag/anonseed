---
title: The per-account service lifecycle is already solved in-family by anonctl's shim @-template unit
slug: per-account-service-lifecycle-solved-by-anonctl-shim-unit
type: finding
source: |
  ../anonctl source, read 2026-07-10 (read-only, no spike run):
  - internal/systemd/systemd.go (TemplateUnit / InstanceName / EnvFile): anonctl
    ships a per-account systemd @-template unit `anonctl-shim@<account>.service`,
    root-launched then dropped to the account's uid via `setpriv --reuid`,
    Restart=on-failure, WantedBy=multi-user.target, EnvironmentFile per instance,
    installed under /etc/systemd/system (DefaultUnitDir).
  - use_exec.go (enterAccount): a shared verify-then-enter `use` primitive, noted
    as "the shape a future shared anoncore enter-primitive will host".
  Provenance strength: STRONG (primary source: anonctl's shipping code). The
  SearXNG-unit variant is reasoned from this precedent, not yet run (deliberately:
  decision A \u2014 skip the running-spike because the mechanism is already proven).
---

# Per-account "ensure SearXNG is running": solved by precedent, not a new problem

This closes the shape-B lifecycle question (`work/notes/ideas/searxng-socket-wired-seed.md`) WITHOUT a running-spike, because the family already ships the exact mechanism.

## The mechanism already exists and is proven: anonctl's shim @-template unit

anonctl supervises EACH account's forced-egress shim as a per-account systemd `@`-template unit (`internal/systemd/systemd.go`):

- ONE account-agnostic template (`anonctl-shim@.service`); `systemctl enable --now anonctl-shim@<account>` supervises that account's process.
- **Root-launched, then dropped to the account** via `ExecStart=/usr/bin/setpriv --reuid ${UID} --regid ${UID} --clear-groups <bin> ...` (the unit starts as root only long enough to drop; the process never runs as root).
- Per-account parameters come from a per-instance `EnvironmentFile` (`<envdir>/%i.env`), so one template serves every account.
- `Restart=on-failure`, `RestartSec=2`, `WantedBy=multi-user.target` \u2014 always-on, reboot-persistent.
- Installed under `/etc/systemd/system`.

**A per-account SearXNG worker (B1/B2) is the SAME pattern with a different `ExecStart`:** `setpriv --reuid <account-uid> ... uwsgi --ini <per-account>` (or the container equivalent for a future B3). So "can we keep a per-account service running?" is already answered YES, demonstrably, by shipping code. There is nothing new to prove by running it \u2014 hence decision A (skip the running-spike).

## Decisions this settles

1. **Lifecycle owner = anonctl / anoncore, NOT anonseed.** A per-account supervised process is exactly the class of thing anonctl already owns (the shim unit lives in anonctl's `internal/systemd`, and the `enterAccount`/`use` primitive is heading into anoncore). A SearXNG-per-account unit belongs in the SAME home. anonseed stays a pure SEEDER: it seeds the webveil config (pointing at the per-account socket) and, at most, DECLARES that the account wants a SearXNG unit; anonctl/anoncore INSTALLS and SUPERVISES the unit. This keeps the spec's Q2(ii) pin intact (the seed interface stays declarative; the running lives outside anonseed).

2. **Always-on (shim-style) is the default; start-on-`use` is a later optimization.** The shim is always-on + restart-on-failure + reboot-persistent. Matching that for SearXNG is the path of least resistance and needs no new pattern. Start-on-`use` (socket activation, or the `enterAccount` primitive bringing SearXNG up on entry and down after) is a real option the operator raised, but it is an OPTIMIZATION over the proven always-on baseline, not a prerequisite.

3. **The env-file split carries over.** The shim's per-instance `EnvironmentFile` pattern maps directly onto SearXNG's per-account parameters (socket path, `SEARXNG_SETTINGS_PATH`, `TMPDIR`/cache dir, uid) \u2014 the exact per-account trio the B1 spike surfaced (`searxng-per-account-instance-b1-proof.md`). So a `searxng@<account>.service` + `<account>.env` is a near-mechanical analogue of the shim unit + env file.

## What is NOT proven (honest scope)

- The SearXNG-unit variant was NOT run against a real account (decision A). The mechanism is proven for the shim; the SearXNG `ExecStart` is a reasoned analogue. A B-implementation task should still smoke-test the actual `searxng@<account>.service` on a real anon account before shipping.
- Ownership landing in anoncore assumes the per-account-unit machinery is extracted there (the shim unit is anonctl-local today; anoncore already hosts account/provision/seedhome and is slated to host the enter-primitive). Whether anonseed DECLARES-and-anonctl-supervises, or the unit machinery is shared via anoncore, is an anoncore-boundary decision to confirm when B is built.
