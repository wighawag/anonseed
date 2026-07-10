---
title: A socket-wired local SearXNG for a webveil-capable seed (no --allow exemption needed)
slug: searxng-socket-wired-seed
type: idea
status: incubating
---

# Socket-wired SearXNG under the anonctl model

An exploration for wiring anonymized web search (SearXNG via pi-webveil) into the pi seed. Recorded from a design discussion; the Phase-1 feasibility spike is DONE (see `work/notes/findings/webveil-searxng-unix-socket-contract.md`), and the socket path is proven. The remaining open work is shape (B)'s LIFECYCLE (who runs the per-account SearXNG). See also the parent prd `work/prds/ready/anonseed-config-seeder.md`.

## Status / decisions so far (2026-07-10)

- **Phase-1 spike DONE, no mutation.** The load-bearing unknown ("can pi-webveil talk to SearXNG over a socket file?") is ANSWERED YES from the operator's live config + webveil source. Details + provenance in the finding `webveil-searxng-unix-socket-contract.md`. Shape (A) has zero feasibility risk; shape (B)'s socket mechanics are trivial, only its lifecycle is open.
- **Not folded into the prd yet — deliberately WAITING FOR (B).** The prd is unchanged. We resolve (B)'s lifecycle FIRST, then choose A-vs-B knowing, then fold the outcome into the prd. (If (B) wins and lands in the prd, story 22 and the Out-of-Scope SearXNG line will need updating — see the boundary-reversal note below.)
- **Phase-2 prototype: NOT the next step.** For shape (A) a prototype earns nothing (the config is 4 known keys webveil already parses). The next step for (B) is a DESIGN decision on the lifecycle owner, not code.

## Boundary reversal to record (webveil becomes a DEFAULT, not a future-seed)

The original pinned design had the v1 pi seed seed ONLY the local model, with webveil/SearXNG explicitly OUT of scope (anon-pi's stance: an extension set is an identity fingerprint, extensions run code and can leak, webveil needs a running SearXNG). This is now DELIBERATELY REVERSED for the pi seed: **the pi seed should wire webveil BY DEFAULT**, because an agent that cannot search the web is very limited. The reversal is defensible here because (a) it is just webveil (a known quantity), not the user's whole extension set, and (b) SearXNG runs locally and anonctl forces the account's egress, so there is no new leak. webveil remains DISABLE-ABLE during seeding (see the decision tree). When this is folded into the prd, prd story 22 ("seed ONLY the local model, never extensions") and the Out-of-Scope SearXNG bullet must be updated to match.

## The seeding decision tree (small set of questions at seed time)

The pi seed asks the operator which SearXNG shape applies, defaulting to wiring webveil:

1. **Embed a per-account SearXNG (shape B)?** → the richer, isolated path: each account gets its own SearXNG bound to its own socket. Preferred once (B)'s lifecycle is solved.
2. **Else, already have a SearXNG installed (shape A)?** → point webveil at the existing/configurable socket path (default `unix:/usr/local/searxng/run/socket`). Pure config write, zero lifecycle.
3. **Else (unwilling to embed B AND no existing SearXNG A)** → seed a SIMPLER pi setup: model-only, no webveil. A cripple-but-clean fallback, chosen explicitly, not silently.

## The problem

A webveil-style capability needs a running SearXNG for web search. In anon-pi's netcage model, SearXNG could live inside the container image, and its own outbound crawl egress was force-proxied by netcage, so `egress: direct` was correct in-jail. The anonctl model is different: there is NO container image and NO per-process egress jail wrapping a bundled service. So how is anonymized web search provided?

## The key insight: a Unix socket needs no exemption

anonctl's `--allow` guardrail exempts a destination by **IP:port** in the nftables egress ruleset (a `:port` is mandatory; there is deliberately no all-ports form; loopback `127.0.0.1:<port>` is accepted via a stricter guardrail that blocks anonymizer control/SOCKS/DNS ports).

A **Unix domain socket has no IP and no port, and never leaves the host**. There is therefore NOTHING for the kernel egress-forcing rule to match on, and NO `--allow` exemption is required (or even possible) for a tool talking to a local SearXNG over a socket file. This is cleaner than the alternative of exempting SearXNG at an `IP:port`:

- **Option (a): `--allow` to an existing SearXNG at IP:port.** Needs a second `--allow` hole; the anonymity of that path becomes the user's SearXNG's responsibility (the user must have wired it to an anonymizing proxy themselves).
- **Option (b): a LOCAL socket-wired SearXNG.** Zero exemptions. SearXNG's OWN outbound crawl egress is already forced through the proxy by anonctl's per-UID kernel forcing (exactly the property anon-pi relied on via netcage), so SearXNG needs no anonymization of its own. Because there is no port to expose, a plain local install talking over a socket file may be genuinely easy.

## The shape landscape (B0-B3), after the Phase-2 spike

The Phase-2 spike PROVED B1 works (see `work/notes/findings/searxng-per-account-instance-b1-proof.md`). Here is the full landscape, in increasing account-ownership / decreasing host-dependency. A key axis your review added: **B0/B1/B2 all REQUIRE the user to have SearXNG installed on the host (and the default install layout)**, which is less than ideal; B3 removes that dependency.

| Shape | SearXNG source | New process per account? | Lifecycle owner | Host dep | Search-history isolation |
| --- | --- | --- | --- | --- | --- |
| **B0** (=A) | shared HOST install, ALREADY RUNNING | **NO** — reuse the running process/socket | none (already running) | needs host SearXNG | **NONE** (shared process/cache/exit) |
| **B1** | shared HOST install (reused code+venv) | **YES** — one extra uwsgi worker per account | the host uwsgi (drop a `.ini` in `apps-enabled/`) | needs host SearXNG | own process+cache+per-UID exit |
| **B2** | shared HOST install | **YES** — as B1, under the account's OWN unit | account-owned systemd/user unit (linger) | needs host SearXNG | as B1, account-owned |
| **B3** | SearXNG INSIDE the account (container image) | **YES** — a per-account container | a podman container (per account) | NONE (self-contained) | fully self-contained |

### The load-bearing distinction (corrects the earlier framing): reuse-of-FILES vs reuse-of-PROCESS

B0/A is the ONLY shape that reuses the running SearXNG PROCESS — every account points webveil at the same already-running socket, nothing new runs, zero lifecycle. **B1/B2/B3 all require a SEPARATE per-account SearXNG process to be running** (one shared + one per active account). B1/B2's "reuse" is reuse of the installed FILES (code + venv, no reinstall) ONLY, NOT reuse of the running process: the per-account worker does not exist until something starts it. So on a shared substrate you STILL need a runner per account for B1/B2/B3 — the spike proved this is possible, not that it is free.

### B1 (proven) — reuse the installed SearXNG as an extra worker

Proven end to end (HTTP 200, 33 results over a per-instance socket). Reuses the shared, world-readable code + venv (no reinstall); each account gets its own socket + settings (`secret_key`) + **writable cache dir** (the spike's surprise finding: SearXNG needs a per-instance writable SQLite cache, default in tmpdir, or instances collide) + uid. Lifecycle on THIS host = drop a per-account `.ini` into `/etc/uwsgi/apps-enabled/` and reload. That is writing HOST service state (`/etc/uwsgi`), which pushes past a strictly-declarative home-seed and REOPENS the prd's Q2(ii) pin.

### B3 — SearXNG inside the account (container): SPLIT by substrate (anonbox trivial, anonctl heavy)

B3 is NOT one thing; it differs completely by substrate:

- **anonbox-B3: TRIVIAL and already proven.** anonbox is the anon-pi/netcage model: a Dockerfile with SearXNG (and webveil) already installed AND running in the image. We KNOW this works because anon-pi already ships exactly this via netcage. For the anonbox target, "B3" is just "the image has it" — no new lifecycle, no new spike. This is the natural home for webveil under anonbox.
- **anonctl-B3: HEAVY and awkward — PARKED as an idea.** Under anonctl there is no image substrate, so B3 means adding a container that RUNS as part of the account's `use`, containing SearXNG ONLY (webveil lives in the account's pi config, not the container). Two problems: (1) it introduces a per-account container lifecycle under a substrate that otherwise has none; (2) reusing SearXNG's OFFICIAL docker image exposes a **TCP port**, not a Unix socket — which REINTRODUCES the port/`--allow`/port-collision complexity the whole socket approach existed to avoid. So anonctl-B3 loses the socket elegance and is heavier than B1/B2. **Decision: park anonctl-B3 as an idea; do NOT pursue it for the anonctl target now.**

### The chosen anonctl path: lower B1/B2 friction via install-detection

Instead of shipping a container under anonctl (B3), make B1/B2 easy:

- We CANNOT install SearXNG on the host for the user (its install method changes across versions; we would be chasing upstream). So we make it EASY for the user to install SearXNG, and the SEED DETECTS an existing SearXNG install + its settings and wires to it (B1/B2). The "user must have SearXNG" precondition is softened by good DETECTION + guidance, not by shipping a container.
- So the pi seed becomes install-aware: check for a host SearXNG (its `searxng-src`/venv + `settings.yml`), and if present, seed webveil against a per-account instance (B1/B2). If absent, guide the user to install it (or fall back to the model-only simpler pi, per the decision tree).

## Recommended decision framing (updated after the lifecycle finding)

Per-substrate, the picture is now:

- **anonbox target:** webveil rides the IMAGE (anonbox-B3), exactly as anon-pi does today via netcage. Trivial, proven. Nothing to design.
- **anonctl target:** the path is **B0/A when the operator accepts shared search, else B1/B2 (install-detected) when per-persona search-unlinkability matters**. anonctl-B3 (container) is PARKED (heavy, port-reintroducing).
  - **B0/A** — trivial, zero new process, but shared search history/cache/exit across personas (see Q2 answer). Fine for a single-persona box or when cross-persona search correlation is acceptable.
  - **B1/B2** — per-persona SearXNG (own process + cache + per-UID exit) for search-history unlinkability. Needs a per-account runner; that runner's lifecycle is SOLVED by precedent (see below).

### Lifecycle owner — ANSWERED (finding `per-account-service-lifecycle-solved-by-anonctl-shim-unit.md`)

"How do we ensure the per-account SearXNG is running?" is already solved in-family: anonctl ships a per-account systemd `@`-template unit for the shim (`anonctl-shim@<account>.service`, root-launched then dropped to the account's uid via `setpriv`, restart-on-failure, reboot-persistent, per-instance env file). A `searxng@<account>.service` is the SAME pattern with a different `ExecStart`. Conclusions:

- **Owner = anonctl / anoncore, NOT anonseed.** Per-account supervised processes are the class anonctl already owns; anonseed stays a pure seeder (seeds webveil config + at most DECLARES the account wants SearXNG; anonctl/anoncore installs+supervises the unit). Keeps the prd Q2(ii) declarative-interface pin intact.
- **Always-on (shim-style) is the default;** start-on-`use` is a later optimization, not a prerequisite.

### Open questions remaining (smaller now)

1. **anoncore boundary:** does anonseed DECLARE "wants SearXNG unit" and anonctl supervise, or is the per-account-unit machinery extracted into anoncore (which already hosts account/provision/seedhome + the slated enter-primitive)? Confirm when B is built.
2. **Does per-account isolation actually BUY anonymity?** — ANSWERED: YES, it buys SEARCH-HISTORY UNLINKABILITY across personas, and this is the real reason to prefer per-account, NOT egress IP. anonctl already forces every account's egress per-UID, so all accounts already egress anonymized regardless. The gap B0/A leaves is that ONE shared SearXNG process sees ALL accounts' queries mixed, keeps ONE shared query cache, and (on this host) crawls through ONE shared `outgoing.proxies` exit — so persona A's and persona B's search histories are CROSS-REFERENCEABLE at the shared SearXNG (same process, same cache, same exit). A per-account SearXNG (B1/B2/B3) gives each persona its own process + cache + per-UID forced exit, i.e. per-persona unlinkable search matching the per-persona unlinkable egress anonctl already provides. So: if you care about cross-persona search correlation → per-account (prefer B3 for no host dep). If a box runs one persona, or shared search history is acceptable → B0/A is genuinely fine (operator's call: "that might indeed be fine").
3. **B3 home: pi seed or anonbox target?** — ANSWERED (per substrate): anonbox-B3 is the anonbox target's image-staging (SearXNG baked in, trivial, proven). anonctl-B3 (container under anonctl) is PARKED (heavy, port-reintroducing); the anonctl path is install-detected B1/B2.
4. **Q2(ii) pin** — ANSWERED: kept declarative. anonseed seeds config (and, for anonbox, declares the image to stage); the RUNNING lives in anonctl/anonbox (the shim `@`-template-unit pattern owns the per-account SearXNG service). The pin survives.
5. **Two commands vs one `--target`?** — ANSWERED: ONE command per seed type (`anonseed pi`), substrate via `--target {anonctl,anonbox}` (default: interactive detect-then-ask, may seed multiple applicable targets). Seed-type and target are orthogonal; a seed declares `Targets()` (a seed need not apply to both). See prd fork 3 + stories 22f-22g.

## Relationship to the current prd — FOLDED IN (2026-07-10)

The prd HAS now been updated (this note is the design record behind those edits). The fold-in: (a) webveil is a DEFAULT, disable-able part of the pi seed (prd forks 4-6, stories 22a-22e), with the decision tree above; (b) prd story 22 rewritten ("no other provider / no arbitrary extensions; webveil the one exception") and the Out-of-Scope SearXNG bullet replaced with "anonseed does not INSTALL SearXNG (detects + guides)"; (c) the Q2 declarative-interface pin CONFIRMED (running lives in anonctl/anonbox), and the interface refined to be TARGET-AWARE (`Plan(ctx,opts,target)` + `Targets()`) because the webveil portion of the plan differs by substrate; (d) the `--target` one-command decision pinned (prd fork 3). anonctl-B3 is parked (prd Deferred). The ONLY residual open item is the anoncore boundary for the service unit (open question 1 above), a when-B-is-built confirmation, not a v1 blocker.
