---
title: anonseed — a config-seeder for anonymized identities (pi seed first)
slug: anonseed-config-seeder
---

> Launch snapshot — records intent at creation, NOT maintained. Current truth: `docs/adr/` (decisions) + the code; remaining work: `work/tasks/ready/` tasks. (The technical-detail sections below are trimmed by `to-task` once the work is tasked — they move into tasks/ADRs and this spec settles to its durable framing: Problem / Solution / User Stories / Out of Scope.)

## Resolved forks (settled at launch)

The design forks surfaced during authoring, and how each was settled. All v1-blocking forks are resolved, so this spec is agent-taskable for its v1 (anonctl-target) scope; the two DEFERRED items below are future-seed concerns the v1 slice does not depend on (see "Deferred / future-seed decisions").

1. **seed-home helper — RESOLVED: import anoncore's `seedhome`, do not vendor.** `anoncore` is published (v0.1.0) and anonctl's latest imports it (`go.mod`: `require github.com/wighawag/anoncore v0.1.0`). It ships the `seedhome` package: `seedhome.Seed(ctx, runner, templateDir, home, account, force)` strips setuid/setgid/sticky bits on copy, refuses symlinks, writes mode-700, treats a collision as a loud error unless `force`, and does an atomic collision check (writes nothing if any collision). anoncore's README explicitly names anonseed as a planned consumer. So anonseed **imports `github.com/wighawag/anoncore/seedhome`** for the safe-write surface (stories 6-7) and reuses `account` (ResolveAccount / the `anon`/`anon-<name>` vocabulary), `endpoint` (socks5h parsing + credential-free-at-rest guard), and `marker` as needed. NOTE the two distinct guards: anoncore's seedhome enforces the FILE-level credential-shedding (setuid/symlink/mode-700, the uid-transition-escape closure); the API-KEY credential guard (refuse a real-looking model apiKey) is a SEPARATE, pi-seed-owned concern that anoncore's generic seeder knows nothing about — anonseed carries it itself (mirroring anon-pi's `apiKeyLooksReal`).

2. **The built-in-seed interface — RESOLVED (pinned).** anoncore supplies the machinery BELOW a seed (seedhome, account resolution, endpoint parsing) but NOT the shape of a *seed type* as an anonseed abstraction, so this repo pins it. The SETTLED shape is a declarative, TARGET-AWARE plan:

   ```
   type Seed interface {
       Name() string
       Targets() []Target                    // which substrates this seed applies to (a seed need not support both)
       Plan(ctx, opts, target Target) (SeedPlan, error)  // PURE synthesis for a GIVEN target; no I/O, no interactivity
   }
   type SeedPlan struct {
       Files      []FileToWrite   // home-relative path + content
       Exceptions []Exception     // the `--allow IP:port` hole(s) — a LIST (may be empty for a socket-wired service)
   }
   ```

   The pinned decisions: **(i)** `Exceptions` is a LIST, not a single value — it supports more than one hole AND zero (a socket-wired service needs none). **(ii)** The interface is STRICTLY DECLARATIVE (files + exceptions only); any "stage/launch a companion service" concern is pushed OUT of the seed interface (the SearXNG per-account service lifecycle is anonctl/anoncore's job, not the seed's — see fork 5). **(iii)** The interactive step (probe the endpoint, let the user pick which models to import and the default) is SEPARATE and UPSTREAM of the pure `Plan` synthesis (as anon-pi does), keeping `Plan` deterministic and unit-testable. **(iv)** `Plan` takes the TARGET as an input, because a seed's plan can legitimately DIFFER by substrate (e.g. the pi seed's webveil config points at an image-baked SearXNG for anonbox vs an install-detected per-account SearXNG for anonctl — see fork 6); the MODEL portion is target-agnostic, the webveil portion is target-shaped. **(v)** A seed declares `Targets()` because seed-type and target are ORTHOGONAL axes and a seed need not apply to every substrate. `SeedPlan` is JSON-serializable so the deferred PATH-plugin contract can reuse it verbatim (a plugin emits a `SeedPlan` JSON on stdout).

3. **One command per seed type, substrate as `--target` — RESOLVED (pinned).** A seed type is ONE command (`anonseed pi`); the substrate is a `--target {anonctl,anonbox}` PARAMETER, NOT a separate command. This keeps seed-type and target orthogonal (matching the `SeedPlan` design: the seed is substrate-agnostic, the shared machinery applies the target). anonbox's image-staging is modelled as "the anonbox target does MORE with the same seed plan" (the same home/exception seeding AND additionally staging the tool image), not as a different command.
   - **Default (no `--target`) = INTERACTIVE, not silent auto-pick:** detect which substrates are present, then ASK the user. May seed AS MANY APPLICABLE targets as are present (e.g. both anonctl and anonbox), bounded by the seed's `Targets()` (a seed that does not apply to a present substrate is simply skipped for it). An explicit `--target` overrides the prompt. This resolves the earlier auto-detect-vs-explicit sub-question toward interactive-detect-then-ask.

4. **webveil (anonymized web search) is a DEFAULT part of the pi seed — RESOLVED (a deliberate reversal).** The original design seeded ONLY the local model (webveil out of scope), on anon-pi's "an extension set is an identity fingerprint" reasoning. This is DELIBERATELY REVERSED for the pi seed: **it wires webveil (SearXNG) BY DEFAULT**, because an agent that cannot search the web is severely limited. Defensible here because (a) it is just webveil (a known quantity), NOT the user's whole extension set, and (b) SearXNG runs locally and anonctl forces the account's egress per-UID, so there is no new leak (SearXNG's crawl is anonymized by the environment). webveil is DISABLE-ABLE at seed time. Grounded by the spike: `work/notes/findings/webveil-searxng-unix-socket-contract.md` (webveil already consumes a `unix:` socket baseUrl; `egress: direct` is REQUIRED for a socket backend).

5. **The per-account SearXNG service lifecycle is anonctl/anoncore's job, NOT anonseed's — RESOLVED (by precedent).** Ensuring a per-account SearXNG runs is the SAME class of problem anonctl already solves with its per-account systemd `@`-template unit (`anonctl-shim@<account>.service`, root-launched then `setpriv`-dropped to the account uid, restart-on-failure, reboot-persistent, per-instance env file). A `searxng@<account>.service` is that pattern with a different `ExecStart`. So the RUNNING lives in anonctl/anoncore; anonseed stays a pure SEEDER (seeds webveil config + declares the account wants SearXNG). Keeps the fork-2 declarative-interface pin intact. Always-on (shim-style) is the default; start-on-`use` is a later optimization. Grounded by `work/notes/findings/per-account-service-lifecycle-solved-by-anonctl-shim-unit.md`.

6. **How webveil's SearXNG is provided — RESOLVED per substrate (this is why `Plan` is target-aware, fork 2(iv)).**
   - **anonbox target:** webveil + SearXNG are baked into the IMAGE and already running (the anon-pi/netcage model, proven). The seed writes webveil config pointing at the in-image socket; nothing to install/detect. Trivial.
   - **anonctl target:** webveil config points at a SearXNG reached over a per-account Unix SOCKET (no `--allow` needed: a socket has no IP/port). Two shapes: **B0/A** — point at an EXISTING shared host SearXNG socket (trivial, zero new process, but personas' search histories share one process/cache/exit); **B1/B2** — an install-DETECTED per-account SearXNG worker (own process + cache + per-UID exit = per-persona search-unlinkability), reusing the host install's code+venv, supervised by the fork-5 unit. The seed DETECTS an existing SearXNG install + settings and wires accordingly; anonseed does NOT install SearXNG (its install method changes upstream) — it detects + guides. A container-under-anonctl shape ("anonctl-B3") is PARKED as an idea (heavy; reusing the official SearXNG image reintroduces a TCP port, defeating the socket approach). Full landscape: `work/notes/ideas/searxng-socket-wired-seed.md`.

## Problem Statement

An operator who wants to run a local-service-using tool (e.g. `pi` wired to a LAN/loopback model) under an anonymized identity faces a config-provisioning problem, separate from the egress-jailing problem the rest of the family solves. The egress jail (anonctl at the kernel per-UID, netcage per network-namespace) forces all traffic through a proxy fail-closed — but the tool still needs its own config seeded into the anonymized identity's home, AND it needs exactly one direct-egress hole punched for its local model (so the tool can reach the LAN model directly while everything else stays proxied). Doing this by hand is fiddly and, worse, DANGEROUS: naively copying the operator's own tool config into the anonymized home drags the operator's real credentials along, so the "anonymized" identity still authenticates AS the operator — it stops the IP leak while remaining identity-linked, defeating the whole point.

The sibling tool anon-pi solved exactly this for `pi` (as the provisioning half of its launcher), but anon-pi is a pi-specific launcher being retired. The provisioning knowledge should live in a dedicated, tool-agnostic, config-seeding tool that other tools can extend.

## Solution

`anonseed`, a Go CLI, that SEEDS the config a specific local-service-using tool needs into an anonymized identity and DECLARES the one direct-egress exception that tool needs, so the tool is ready to run anonymized. It is config-seeding only: NOT an account provisioner (anonctl/anonbox own accounts) and NOT a runtime launcher (that was anon-pi's other half, being retired).

Invocation is per seed type: `anonseed pi ...` seeds `pi`. Given a seed type, anonseed (1) writes that tool's config files into a target identity's default-home, and (2) declares the direct-egress `--allow IP:port` exception that tool needs. It targets the anonctl substrate today (`/etc/anonctl/default-home/` + `/etc/anonctl/defaults.json`, buildable now against anonctl's shipped hooks) and reserves an anonbox target for later (same home / exception seeding PLUS staging the tool's container image, since anonbox is image-based like netcage).

The load-bearing safety property is the credential-shedding guard: anonseed refuses to seed a real-looking credential into an anonymized home unless explicitly forced. A genuinely local model ignores its apiKey (a placeholder is fine); a real key is refused loudly.

anonseed does NOT re-implement an egress prover — the anonymity proof stays netcage's / anonctl's `verify`. anonseed's own acceptance is its tests plus a guarantee that a seeded home never contains a real credential.

## User Stories

### The tool shape and family placement

1. As an operator, I want a single Go CLI `anonseed` with per-seed-type subcommands (`anonseed pi …`), so that seeding an anonymized identity is one command per tool.
2. As an operator, I want anonseed to be config-seeding ONLY (not provision accounts, not launch runtimes), so that its responsibility is narrow and it composes cleanly with anonctl/anonbox (accounts + egress jail) and the tools it seeds.
3. As a maintainer, I want ONE anonseed repo with MULTIPLE built-in seed types (pi first, opencode and others later) rather than a repo per tool, because the shared surface (resolve the target identity, safely write files into a home, declare the `--allow` exception(s), enforce the credential guard) is the bulk of the work and is identical across seed types, while a seed type itself is small (config knowledge, not machinery).
4. As a Linux operator, I want anonseed to run on Linux and be written in Go, matching the family's platform and stack.
5. As an operator on a box that also runs anonctl/anon-pi, I want anonseed's naming to follow the family convention (run-together, no hyphen: anonctl / anonbox / anonseed / anoncore), so the tools read as one family anchored on the published name anonctl.

### The shared seeding surface

6. As a seed author, I want a shared "resolve the target identity" step (reusing anoncore's `account` vocabulary), so every seed type writes into the right home the same way (the anonctl box-wide default-home, or a specific anon account's home).
7. As an operator, I want anonseed to write files into a home SAFELY by importing anoncore's `seedhome` package: stripping setuid/setgid/sticky bits, refusing symlinks, enforcing mode-700, and treating a collision as a loud error unless forced — so seeding cannot be turned into a privilege-escalation or a leak vector, and this hardening cannot drift from anonctl's (it is the SAME code).
8. As an operator, I want anonseed to DECLARE the direct-egress `IP:port` exception(s) a seed needs by writing into the anonctl target's `allow` list (config key `"allow"`, in `/etc/anonctl/defaults.json`), validated through anonctl's own `--allow` guardrail (public addresses / hostnames / `:53` / port-omitted rejected; loopback `127.0.0.1:<port>` accepted via a stricter check), so a seeded default is never a quieter path to a leak.
9. As an operator, I want the API-KEY credential guard enforced on every seed that carries a key: anonseed must NEVER write a real-looking credential (API key/token) into an anonymized home, and must refuse loudly unless I pass an explicit force flag — because an anonymized identity carrying my real credentials still authenticates AS me, defeating the anonymization. (This is DISTINCT from anoncore seedhome's file-level credential-shedding of story 7: the apiKey guard is anonseed-owned, mirroring anon-pi's `apiKeyLooksReal`; the setuid/symlink/mode-700 guard is anoncore's.)
10. As an operator, I want root-requiring operations (writing under `/etc/anonctl`) to SELF-ELEVATE (mirroring anonctl's stance), rather than printing commands for me to paste, so seeding is one command that does the privileged work itself.

### The anonctl target (buildable now)

11. As an operator, I want anonseed to seed into anonctl's box-wide default-home `/etc/anonctl/default-home/` (the template every fresh anon account inherits), so a bare `anonctl add <name>` afterward lands a ready-to-use anonymized account.
12. As an operator, I want anonseed to declare the direct exception by writing into anonctl's `/etc/anonctl/defaults.json` `"allow"` list, so the box-wide default carries the exemption.
13. As an operator, I want to alternatively seed a SPECIFIC already-provisioned anon account's home (not just the box-wide default-home), so I can wire up an existing account without re-seeding the template. (The anonctl model is home-on-host, with no container image — unlike the netcage/anonbox image-based model.)
14. As an operator, I want anonseed to be create-only / never destructive on the anonctl target (never overwriting an existing seeded file without an explicit force), consistent with anonctl's own create-only `add`.

### The pi seed (v1, FULL scope — mirrors anon-pi's model-config half)

15. As an operator, I want `anonseed pi` to take the local model endpoint's `host:port`, probe that endpoint's live `/v1/models`, AND read the provider in my own `~/.pi/agent/models.json` whose baseUrl matches that endpoint, so the seed reflects both what the server actually offers and my hand-tuned provider settings.
16. As an operator, I want to PICK which discovered models to import and which is the default, so the seed carries exactly the models I want.
17. As an operator, I want the pi seed to generate a `models.json` carrying exactly ONE provider — pointed at the local endpoint, api `openai-completions`, baseUrl the LAN/loopback endpoint, apiKey a PLACEHOLDER — into the target home's `~/.pi/agent/`, so the anonymized pi is wired to my local model and nothing else.
18. As an operator, I want the pi seed to generate a `settings.json` (defaultProvider / defaultModel / enabledModels) into the target home's `~/.pi/agent/`, so pi launches with the right default model selected.
19. As an operator, I want the pi seed to declare the `--allow` exception for that endpoint's `host:port`, so pi can reach the local model directly while all other egress stays forced through the proxy.
20. As a security-conscious operator, I want the pi seed to consider ONLY the provider served by the matched endpoint (never any other provider or key from my config), so no unrelated credential can leak into the seed by construction.
21. As a security-conscious operator, I want the pi seed to REFUSE to seed if the matched provider carries a real-looking apiKey, unless I pass an explicit force flag — because a genuinely local model ignores its apiKey (placeholder fine) but a real key in an anon home re-links the identity to me.
22. As an operator, I want the pi seed to seed ONLY the local model provider (never ANY OTHER provider from my config, never my general extensions or skills) PLUS webveil by default — because an unrelated provider or my whole extension set is an identity fingerprint, but webveil specifically is worth wiring by default (an agent that cannot search is crippled) and carries no new leak (local SearXNG + anonctl-forced egress). So the exclusion is "no other provider, no arbitrary extensions"; webveil is the ONE deliberate, disable-able exception (see stories 22a-22d).

### webveil / anonymized web search (default-on, disable-able)

22a. As an operator, I want the pi seed to wire webveil (SearXNG) BY DEFAULT, so the seeded anon pi can search the web, with a flag/prompt to DISABLE it if I do not want it.
22b. As an operator seeding the ANONBOX target, I want webveil + SearXNG to come from the staged IMAGE (already installed and running), so nothing needs installing on the host and webveil points at the in-image socket. (Proven model: anon-pi via netcage.)
22c. As an operator seeding the ANONCTL target, I want webveil pointed at a SearXNG over a per-account Unix SOCKET (no `--allow` exemption, since a socket has no IP/port), with the seed DETECTING an existing host SearXNG install + settings and wiring either the shared socket (B0/A) or an install-detected per-account instance (B1/B2). anonseed detects and guides; it does NOT install SearXNG itself.
22d. As an operator with NO SearXNG available and unwilling to add one, I want the seed to fall back to a simpler model-only pi (webveil disabled), chosen explicitly via the decision tree, not silently.
22e. As a security-conscious operator, I want the anonctl webveil config to set `egress: direct` (REQUIRED for a socket backend) and `fetchEgress: direct` (because anonctl forces the account's egress at the kernel), so both SearXNG's crawl and webveil's web_fetch are anonymized by the environment, not by webveil's own proxying.

### The `--target` substrate axis

22f. As an operator, I want `anonseed pi` to accept `--target {anonctl,anonbox}` selecting the substrate, defaulting to interactively detecting which substrates are present and asking me (optionally seeding as many applicable targets as are present), so one command serves both substrates without a separate binary.
22g. As a seed author, I want a seed to declare which `Targets()` it supports, so a seed that does not apply to a present substrate is skipped for it rather than mis-seeded.

### The anonbox target (deferred / stubbable)

23. As a maintainer, I want the anonbox target designed for but stubbable/deferred in v1 (since anonbox does not exist yet), doing the same home / exception seeding AND additionally providing or staging the container image that has the tool installed, so anonseed is ready for the image-based substrate without blocking on anonbox.

### The reserved PATH-plugin escape hatch (designed, NOT built)

24. As a maintainer, I want a git/kubectl-style PATH-plugin escape hatch RESERVED (designed for, not built): `anonseed foo` falls back to exec-ing a PATH executable `anonseed-foo` when `foo` is not a built-in — so third-party seeds are possible later without changing the core. The discovery mechanism is NOT built in v1 (speculative until a third-party seed exists); its argv/stdout contract is DEFERRED (see "Deferred / future-seed decisions"), with the one constraint that `SeedPlan` is JSON-serializable so the plugin can emit it on stdout.

### Acceptance / verification

25. As a maintainer, I want anonseed's acceptance to be its OWN Go tests PLUS a check that a seeded home never contains a real credential, so the load-bearing safety property is a test seam, not just a code path.
26. As a maintainer, I want anonseed to NOT re-implement an egress prover — the deeper anonymity proof stays netcage's / anonctl's `verify` — so anonseed stays scoped to config-seeding.

### Autonomy notes (the two gate axes)

- **`humanOnly`:** NOT set at the spec level — tasking this spec does not itself require a human to drive it. (The tasker will still set each task's OWN gate from its build-nature; several tasks here — the api-key guard, the `/etc/anonctl` self-elevation writes — are security-sensitive and the tasker should gate THOSE tasks `humanOnly` at tasking time.)
- **`needsAnswers`: CLEARED (omitted).** All v1-blocking forks are resolved (forks 1-6 above): import anoncore's `seedhome` (1); the target-aware declarative seed interface (2); one-command-plus-`--target` (3); webveil default-on (4); the SearXNG service lifecycle owned by anonctl/anoncore (5); webveil's SearXNG provided per-substrate, anonctl via install-detected socket (6). The ONLY remaining DEFERRED item — the PATH-plugin argv/stdout contract — is a future-third-party-seed concern the v1 slice does not depend on. So this spec is agent-taskable for its v1 scope now.

### Deferred / future-seed decisions (NOT v1 blockers)

Recorded so they are not lost, and so the v1 interface does not accidentally foreclose them. Neither gates tasking of the v1 slice; both are picked up when the relevant future seed is built.

- **PATH-plugin argv/stdout contract — DEFERRED (agreed).** The git/kubectl-style escape hatch (`anonseed foo` execs a PATH `anonseed-foo` when `foo` is not built-in) is RESERVED, not built in v1. Its argv/stdin/stdout contract is deliberately left unpinned until a real third-party seed exists. The one constraint honoured now: `SeedPlan` (Q2) is JSON-serializable, so whenever the plugin is built the contract can simply be "the plugin emits a `SeedPlan` JSON document on stdout" (files to write + `--allow` exceptions to declare). Building the discovery mechanism is speculative until then.
- **webveil / SearXNG is NO LONGER deferred — it is RESOLVED as a v1 default** (forks 4-6, stories 22a-22e). Recorded here only as a pointer: the full spike record lives in `work/notes/ideas/searxng-socket-wired-seed.md` + the three `work/notes/findings/` files (the webveil socket contract, the B1 per-account proof, and the service-lifecycle-by-precedent finding).
- **The anoncore boundary for the SearXNG service unit — small open sub-item.** Does anonseed DECLARE "wants SearXNG" and anonctl supervise, or is the per-account-unit machinery extracted into anoncore? A boundary confirmation for when the anonctl B1/B2 path is built; NOT a v1 blocker (fork 5 already fixes the owner as anonctl/anoncore, not anonseed).
- **anonctl-B3 (SearXNG in a container under anonctl) — PARKED as an idea.** Heavy; reusing the official SearXNG docker image exposes a TCP port, reintroducing the `--allow`/port-collision complexity the socket approach avoids. The anonctl path uses install-detected B1/B2 instead. (anonbox-B3 is different and trivial — the image bakes SearXNG in — see fork 6.)

## Implementation Decisions

Decisions settled at launch (seed to the tasking; trimmed at tasking-time into tasks / ADRs):

- **ONE repo, MULTIPLE built-in seed types as plain subcommands** (pi first). NOT a repo per tool. Rationale: the shared surface is the bulk of the work and identical across seeds; a seed type is small config knowledge.
- **The anonctl target is buildable NOW** against anonctl's shipped hooks: `/etc/anonctl/default-home/` (seed template) and `/etc/anonctl/defaults.json` (config key `"allow"`). Two sub-targets: box-wide default-home OR a specific anon account's home. Home-on-host, no image.
- **The anonctl integration is the ON-DISK FILE CONVENTION, NOT an imported anonctl API.** anonseed WRITES the conventions directly (the `default-home/` dir and `defaults.json`), exactly as anonctl documents them as `cp`-able / dependency-free (anonctl's own logic lives in its Go-INTERNAL `internal/defaults`, un-importable from outside that module). anonseed imports ONLY anoncore (`seedhome`/`account`/`endpoint`/`marker`), never anonctl. So the seam between the two tools is the file layout, deliberately.
- **The `--allow` guardrail VALIDATION must be re-implemented or extracted — it is NOT importable.** Stories 8/12 validate exemptions "through anonctl's guardrail," but that guardrail (the RFC1918/link-local + loopback-stricter + `:53`/port-mandatory checks) lives in anonctl's Go-internal `internal/lanexempt`, which anonseed cannot import. So an implementer must either (a) re-implement the guardrail in anonseed (keeping it byte-aligned with anonctl/netcage's rules), or (b) get it extracted into anoncore (the natural shared home, alongside `endpoint`). Pin this at tasking time; do NOT assume an importable guardrail.
- **The anonbox target is designed but stubbable/deferred** (anonbox does not exist yet). It additionally stages the tool's container image (image-based substrate).
- **Self-elevation for root-requiring ops** (writing under `/etc/anonctl`), mirroring anonctl — NOT printing commands.
- **Two credential guards, kept distinct.** (1) The FILE-level credential-shedding (setuid/setgid/sticky strip, symlink refusal, mode-700) is anoncore's `seedhome` — imported, not re-implemented. (2) The API-KEY guard (never seed a real-looking apiKey/token; refuse loudly unless an explicit force flag; a benign/placeholder key is allowed since a local model ignores it) is anonseed-owned, mirroring anon-pi's `apiKeyLooksReal`. Both are hard requirements AND test seams.
- **The pi seed reads ONLY the provider matched to the endpoint** (in the user's `~/.pi/agent/models.json`) plus the endpoint's live `/v1/models` — by construction no other provider or key can enter the seed.
- **The pi seed wires webveil (SearXNG) by DEFAULT** (disable-able), per-substrate: anonbox from the staged image, anonctl over a per-account Unix socket (install-detected, no `--allow`). The per-account SearXNG service is anonctl/anoncore-supervised, not anonseed's job. See forks 4-6 + `work/notes/ideas/searxng-socket-wired-seed.md`.
- **One command per seed type; substrate via `--target {anonctl,anonbox}`** (default: interactive detect-then-ask, may seed multiple applicable targets). Seed-type and target are orthogonal; a seed declares its `Targets()`. See fork 3.
- **Generated files:** a `models.json` (one provider, api `openai-completions`, baseUrl the endpoint, apiKey a placeholder) and a `settings.json` (defaultProvider / defaultModel / enabledModels), into the target home's `~/.pi/agent/`.
- **The PATH-plugin escape hatch is RESERVED, not built** (`anonseed foo` → exec `anonseed-foo`). Discovery mechanism deferred until a real third-party seed exists.
- **seed-home helper: RESOLVED** — import `github.com/wighawag/anoncore/seedhome` (anoncore v0.1.0 is published; anonctl's latest already imports it). Do NOT vendor. Also reuse anoncore `account` / `endpoint` / `marker` as needed.
- Reference implementation for the pi seed: the sibling repo `../anon-pi`, specifically `packages/anon-pi/src` (its `init` model-config half: `findEndpointProvider`, `apiKeyLooksReal`, the `models.json` synthesis, `--force-allow-local-llm-api-key`) and its README "Worked example: an anonymized pi account wired to a LAN model". anonctl's `/etc/anonctl/default-home/` + `/etc/anonctl/defaults.json` hooks are documented in anonctl's README (the box-wide defaults section).

## Testing Decisions

- The **credential-shedding guard** is the primary test seam: assert a seeded home NEVER contains a real-looking credential, and that a real key is refused (and only admitted with the explicit force flag). Mirror anon-pi's `apiKeyLooksReal` benign-set logic.
- Test the **pi seed's endpoint scoping**: given a `~/.pi/agent/models.json` with several providers, only the one whose baseUrl matches the endpoint is read; no other provider/key enters the generated `models.json`.
- Test the **generated `models.json` / `settings.json` shapes** against the pi config contract (one provider, `openai-completions`, placeholder apiKey; defaultProvider/defaultModel/enabledModels consistent with the picked models).
- Test the **safe-write invariants** (setuid-bit stripping, symlink refusal, mode-700) on the seed-home helper.
- Test **`--allow` declaration** goes through anonctl's guardrail (public / hostname / `:53` / port-omitted rejected; loopback accepted via the stricter check); and that a socket-wired service declares ZERO exceptions (the `Exceptions` list can be empty).
- Prefer testing at the highest seam: the seed subcommand's observable output (files written into a temp home + the exemption declared), not internal helpers, where practical. Do NOT re-implement an egress prover — that is netcage's / anonctl's `verify`.

## Out of Scope

- **Account provisioning** (anonctl / anonbox own accounts) and **runtime launching** (anon-pi's retired other half). anonseed only seeds config.
- **An egress prover / leak-test** — stays netcage's / anonctl's `verify`.
- **Building the PATH-plugin discovery mechanism** — reserved, not built (its contract is DEFERRED; see "Deferred / future-seed decisions").
- **Seeding ARBITRARY pi extensions/skills or any non-matched provider** — deliberately excluded (fingerprint / credential risk). The ONE deliberate exception is webveil, wired by default (forks 4-6): it is a known quantity, carries no new leak (local SearXNG + anonctl-forced egress), and is disable-able. This is not a blanket "seed my extensions" — only webveil, only by the pi seed.
- **Installing SearXNG on the host** — anonseed does NOT install SearXNG (its install method changes upstream; anonseed would be chasing it). It DETECTS an existing install and wires to it, or guides the user to install, or falls back to model-only pi (stories 22c-22d). (webveil/SearXNG itself is now IN scope as a v1 default — forks 4-6 — so it is no longer out-of-scope.)
- **anonctl-B3 / SearXNG-in-a-container-under-anonctl** — parked (see Deferred); the anonctl path is install-detected B1/B2.
- **The per-account SearXNG service unit's implementation** — owned by anonctl/anoncore, not anonseed (fork 5). anonseed seeds config + declares intent; it does not write/supervise the systemd unit.
- **The anonbox image-staging path** beyond a stub — deferred until anonbox exists.
- **A `LICENSE` file** — not written by this scaffold; the repo's default is AGPL-3.0-only (add it in a follow-up if desired).

## Further Notes

- Full design record: the netcage repo's `work/notes/ideas/netcage-machines-scope-fork.md` (its TL;DR and updates 6, 7, 8) holds the family scope-fork rationale. anonctl's README "NOT defended (accepted residual)" section names the host-recon gap this whole family closes.
- Family recap (all under github.com/wighawag, run-together naming): **anonctl** (published — per-UID kernel egress jail), **netcage** (published — per-netns container egress jail), **anonbox** (planned — netcage-backed machine manager), **anoncore** (PUBLISHED, v0.1.0 — shared Go library: account-provisioning, `seedhome`, marker contract, endpoint parsing; anonctl's latest imports it; its README names anonseed as a planned consumer). anonseed is the generalization of anon-pi's provisioning half; anon-pi (the pi-specific launcher) is being retired and its config knowledge becomes anonseed's first seed type.
