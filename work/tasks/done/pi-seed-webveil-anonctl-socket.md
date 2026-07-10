---
title: The pi seed — webveil (SearXNG) default-on for the anonctl target
slug: pi-seed-webveil-anonctl-socket
prd: anonseed-config-seeder
blockedBy: [pi-seed-model-config]
covers: [22, 22a, 22c, 22d, 22e]
---

## What to build

webveil (anonymized web search) wired BY DEFAULT into the pi seed, for the ANONCTL target, disable-able. The pi seed additionally emits a webveil `config.json` into the target home at webveil's XDG config path — `$XDG_CONFIG_HOME/webveil/config.json`, falling back to `<home>/.config/webveil/config.json` (this is a DIFFERENT home subtree than the model files, which go under `~/.pi/agent/`; do NOT place the webveil config under `.pi/`) — pointing at a SearXNG reached over a Unix SOCKET — which needs NO `--allow` exemption (a socket has no IP/port; it never leaves the host). The config sets `backend: searxng`, a `unix:<socket>` baseUrl, `egress: direct` (REQUIRED for a socket backend — webveil refuses to proxy a local hop), and `fetchEgress: direct` (because anonctl forces the account's egress at the kernel, so both SearXNG's crawl and webveil's web_fetch are anonymized by the environment).

Seed-time decision tree (interactive, disable-able):
1. Detect a host SearXNG install + its settings. If present, wire webveil at the SearXNG socket — either the shared existing socket, or an install-detected per-account instance's socket (the per-account SearXNG SERVICE UNIT itself is OUT OF SCOPE here — that is anonctl/anoncore's job; this task only writes the webveil config pointing at the socket path).
2. If NO SearXNG and the operator does not want one, fall back to a simpler model-only pi (webveil disabled) — chosen EXPLICITLY, never silently.

anonseed DETECTS + guides; it does NOT install SearXNG (its install method changes upstream) and does NOT write/supervise the SearXNG systemd unit.

End to end: with SearXNG detected (a fixture), the seed adds a webveil `config.json` (unix socket, direct/direct); with none and disabled, it emits the model-only plan; the disable flag/prompt suppresses webveil.

## Acceptance criteria

- [ ] webveil is wired BY DEFAULT (a `config.json` in the plan) but a flag/prompt DISABLES it.
- [ ] The webveil config is written to the XDG path (`$XDG_CONFIG_HOME/webveil/config.json`, else `<home>/.config/webveil/config.json`), NOT under `~/.pi/agent/`.
- [ ] The anonctl webveil config uses a `unix:<socket>` baseUrl, `egress: direct`, `fetchEgress: direct`; it declares NO `--allow` exception for SearXNG (socket needs none).
- [ ] Detection of a host SearXNG install + settings drives the wiring; absent + declined -> explicit model-only fallback (never silent).
- [ ] This task does NOT install SearXNG and does NOT write a SearXNG systemd/service unit (out of scope: anonctl/anoncore owns the per-account service lifecycle).
- [ ] **Shared-write isolation:** SearXNG detection + the webveil config write are behind seams; tests write only to temp fixtures and assert the real webveil/SearXNG config is untouched.
- [ ] Tests cover: default-on wiring (unix socket, direct/direct), disable flag, detected-vs-absent branches, the model-only fallback.

## Blocked by

- `pi-seed-model-config` (extends the same pi seed's `Plan`; webveil is added on top of the model config).

## Prompt

> Goal: wire webveil (SearXNG search) into the pi seed BY DEFAULT for the anonctl target, disable-able. Domain: an agent that cannot search is crippled, so the pi seed wires webveil by default — the ONE deliberate extension exception (it is a known quantity and carries no new leak: SearXNG runs locally and anonctl forces egress). Under anonctl, webveil talks to SearXNG over a UNIX SOCKET, which needs NO `--allow` (a socket has no IP/port).
>
> Ground truth (read these findings): `work/notes/findings/webveil-searxng-unix-socket-contract.md` (webveil consumes a `unix:<socketPath>[:<httpPath>]` baseUrl; `egress: direct` is REQUIRED for a socket backend, else webveil throws; under anonctl set `fetchEgress: direct` too since the kernel forces egress) and `work/notes/ideas/searxng-socket-wired-seed.md`. Webveil resolves its config XDG-style (`$XDG_CONFIG_HOME/webveil/config.json`, fallback `<home>/.config/webveil/config.json` — verify in webveil's `core/config.ts` `resolveConfig`); write the seeded config THERE, a different subtree from the `~/.pi/agent/` model files (the B0/A shared-socket vs B1/B2 install-detected per-account shapes, and the decision tree). The live webveil config shape is `{backend, baseUrl: unix:/..., egress:{mode:direct}, fetchEgress:{mode:direct}}`.
>
> SCOPE FENCE (important): this task writes the webveil CONFIG only. It does NOT install SearXNG (upstream install method varies) and does NOT create/supervise the per-account SearXNG systemd unit — that lifecycle is anonctl/anoncore's job (see `work/notes/findings/per-account-service-lifecycle-solved-by-anonctl-shim-unit.md`). Detect a host SearXNG + settings, wire the config at its socket; if none and the operator declines, fall back to model-only pi EXPLICITLY.
>
> FIRST, check against reality: confirm the pi seed's `Plan` from `pi-seed-model-config` (this extends it) and the webveil config shape from the finding + the live `~/.config/webveil/config.json` on disk.
>
> Test detection branches + the config shape with fixtures; write only to temp. Done = webveil default-on (disable-able), correct socket/direct config, explicit fallback, gate green.
