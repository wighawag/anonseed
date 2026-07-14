---
title: anonseed — a config-seeder for anonymized identities (pi seed first)
slug: anonseed-config-seeder
---

> Launch snapshot — records intent at creation, NOT maintained. Current truth: `docs/adr/` (decisions) + the code; remaining work: `work/tasks/` tasks.
>
> TASKED (2026-07-10): the technical-detail sections (Resolved forks / Implementation Decisions / Testing Decisions) were trimmed at tasking time — that detail now lives in `work/tasks/backlog/*` (what to build) and in the ADRs those tasks write (`docs/adr/`: the seed interface, the `--allow` guardrail layering). This spec has settled to its durable framing: Problem / Solution / User Stories / Out of Scope. Design record for the SearXNG/webveil decisions: `work/notes/ideas/searxng-socket-wired-seed.md` + `work/notes/findings/*`.

<!-- Resolved-forks / Implementation-Decisions / Testing-Decisions were TRIMMED at tasking (see banner). The decision detail now lives in work/tasks/backlog/* and in the ADRs those tasks write (docs/adr/: the seed interface [seed-interface-and-seedplan]; the --allow guardrail layering [allow-exemption-guardrail]). The SearXNG/webveil design record is work/notes/ideas/searxng-socket-wired-seed.md + work/notes/findings/*. -->

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
- **`needsAnswers`: CLEARED (omitted).** The spec launched with all v1-blocking forks resolved (their detail was trimmed at tasking into the tasks + ADRs; see the banner): import anoncore's `seedhome`; the target-aware declarative seed interface; one-command-plus-`--target`; webveil default-on; the SearXNG service lifecycle owned by anonctl/anoncore; webveil's SearXNG provided per-substrate (anonctl via install-detected socket). The ONLY remaining DEFERRED item — the PATH-plugin argv/stdout contract — is a future-third-party-seed concern the v1 slice does not depend on.

### Deferred / future-seed decisions (NOT v1 blockers)

Recorded so they are not lost, and so the v1 interface does not accidentally foreclose them. Neither gates tasking of the v1 slice; both are picked up when the relevant future seed is built.

- **PATH-plugin argv/stdout contract — DEFERRED (agreed).** The git/kubectl-style escape hatch (`anonseed foo` execs a PATH `anonseed-foo` when `foo` is not built-in) is RESERVED, not built in v1. Its argv/stdin/stdout contract is deliberately left unpinned until a real third-party seed exists. The one constraint honoured now: `SeedPlan` (Q2) is JSON-serializable, so whenever the plugin is built the contract can simply be "the plugin emits a `SeedPlan` JSON document on stdout" (files to write + `--allow` exceptions to declare). Building the discovery mechanism is speculative until then.
- **webveil / SearXNG is a v1 DEFAULT** (stories 22a-22e; tasks `pi-seed-webveil-anonctl-socket` + `anonbox-target-stub`). The full spike record lives in `work/notes/ideas/searxng-socket-wired-seed.md` + the three `work/notes/findings/` files (the webveil socket contract, the B1 per-account proof, and the service-lifecycle-by-precedent finding).
- **The anoncore boundary for the SearXNG service unit — small open sub-item.** Does anonseed DECLARE "wants SearXNG" and anonctl supervise, or is the per-account-unit machinery extracted into anoncore? A boundary confirmation for when the anonctl per-account-service path is built; NOT a v1 blocker (the owner is already fixed as anonctl/anoncore, not anonseed).
- **anonctl-B3 (SearXNG in a container under anonctl) — PARKED as an idea.** Heavy; reusing the official SearXNG docker image exposes a TCP port, reintroducing the `--allow`/port-collision complexity the socket approach avoids. The anonctl path uses install-detected B1/B2 instead. (anonbox-B3 is different and trivial — the image bakes SearXNG in — see fork 6.)

<!-- ## Implementation Decisions and ## Testing Decisions were TRIMMED at tasking (2026-07-10). What-to-build now lives in work/tasks/backlog/*; durable rationale is being written as ADRs by those tasks (docs/adr/: the seed interface; the --allow guardrail pre-check-vs-authoritative layering). Reference implementation for the pi seed: ../anon-pi packages/anon-pi/src (findEndpointProvider, apiKeyLooksReal, models.json synthesis). SearXNG/webveil design record: work/notes/ideas/searxng-socket-wired-seed.md + work/notes/findings/*. -->

## Out of Scope

- **Account provisioning** (anonctl / anonbox own accounts) and **runtime launching** (anon-pi's retired other half). anonseed only seeds config.
- **An egress prover / leak-test** — stays netcage's / anonctl's `verify`.
- **Building the PATH-plugin discovery mechanism** — reserved, not built (its contract is DEFERRED; see "Deferred / future-seed decisions").
- **Seeding ARBITRARY pi extensions/skills or any non-matched provider** — deliberately excluded (fingerprint / credential risk). The ONE deliberate exception is webveil, wired by default: it is a known quantity, carries no new leak (local SearXNG + anonctl-forced egress), and is disable-able. This is not a blanket "seed my extensions" — only webveil, only by the pi seed.
- **Installing SearXNG on the host** — anonseed does NOT install SearXNG (its install method changes upstream; anonseed would be chasing it). It DETECTS an existing install and wires to it, or guides the user to install, or falls back to model-only pi (stories 22c-22d). (webveil/SearXNG itself is IN scope as a v1 default — so it is not out-of-scope.)
- **anonctl-B3 / SearXNG-in-a-container-under-anonctl** — parked (see Deferred); the anonctl path is install-detected B1/B2.
- **The per-account SearXNG service unit's implementation** — owned by anonctl/anoncore, not anonseed. anonseed seeds config + declares intent; it does not write/supervise the systemd unit.
- **The anonbox image-staging path** beyond a stub — deferred until anonbox exists.
- **A `LICENSE` file** — not written by this scaffold; the repo's default is AGPL-3.0-only (add it in a follow-up if desired).

## Further Notes

- Full design record: the netcage repo's `work/notes/ideas/netcage-machines-scope-fork.md` (its TL;DR and updates 6, 7, 8) holds the family scope-fork rationale. anonctl's README "NOT defended (accepted residual)" section names the host-recon gap this whole family closes.
- Family recap (all under github.com/wighawag, run-together naming): **anonctl** (published — per-UID kernel egress jail), **netcage** (published — per-netns container egress jail), **anonbox** (planned — netcage-backed machine manager), **anoncore** (PUBLISHED, v0.1.0 — shared Go library: account-provisioning, `seedhome`, marker contract, endpoint parsing; anonctl's latest imports it; its README names anonseed as a planned consumer). anonseed is the generalization of anon-pi's provisioning half; anon-pi (the pi-specific launcher) is being retired and its config knowledge becomes anonseed's first seed type.
