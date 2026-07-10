---
title: pi-webveil already consumes SearXNG over a per-account Unix socket (the seed contract)
slug: webveil-searxng-unix-socket-contract
type: finding
source: |
  Live host config + webveil source, read 2026-07-10 (Phase 1 spike, no mutation):
  - ~/.config/webveil/config.json (the operator's working config)
  - /etc/uwsgi/apps-available/searxng.ini (how SearXNG binds its socket, symlinked into apps-enabled/)
  - /usr/local/searxng/run/socket (the live socket: `srw-rw-rw- www-data root`)
  - ../webveil/packages/webveil/src/core/baseurl.ts (the `unix:` baseUrl grammar + parser)
  - ../webveil/packages/webveil/src/core/egress.ts (the egress vs fetchEgress split + fail-loud guard)
  Provenance strength: STRONG for the webveil/SearXNG contract (primary source: webveil's own
  code and the operator's live, working config). The per-account MULTI-instance feasibility
  (running a SECOND SearXNG on a different socket) is NOT yet verified by running it — see the
  open item at the bottom.
---

# pi-webveil ↔ SearXNG over a Unix socket: the ground-truth contract

Spike finding for the idea `work/notes/ideas/searxng-socket-wired-seed.md` (can a socket-wired SearXNG be part of the DEFAULT pi seed?). The load-bearing external unknown — "can the consumer talk to SearXNG over a socket file?" — is ANSWERED by the operator's live setup and webveil's own source. It works today.

## 1. webveil consumes a `unix:` socket baseUrl (proven, live)

The operator's working `~/.config/webveil/config.json`:

```json
{
  "backend": "searxng",
  "baseUrl": "unix:/usr/local/searxng/run/socket",
  "egress": { "mode": "direct" },
  "fetchEgress": { "mode": "socks5", "url": "socks5h://127.0.0.1:1080" }
}
```

webveil's `baseurl.ts` defines and parses the grammar (a RECORDED decision in webveil, not an accident):

```
unix:<socketPath>[:<httpPath>]
  <socketPath> : absolute path to the uWSGI Unix domain socket (no colon in the path)
  <httpPath>   : OPTIONAL app mount point, defaults to `/`
```

So `unix:/usr/local/searxng/run/socket` is the install default (webveil requests `/search` on it). For the socket form webveil builds a socket-bound undici `Agent({connect:{socketPath}})` scoped to the BACKEND hop only, and rewrites the base to a synthetic `http://localhost` so the searxng backend's URL-building is unchanged.

## 2. SearXNG binds that socket via uwsgi (proven, live)

`/etc/uwsgi/apps-available/searxng.ini` (symlinked into `apps-enabled/`, served by the `uwsgi.service` systemd unit):

```ini
uid = searxng
gid = searxng
chmod-socket = 666
http-socket = /usr/local/searxng/run/socket
module = searx.webapp
env = SEARXNG_SETTINGS_PATH=/etc/searxng/settings.yml
```

Key point: the socket path is a single ini line (`http-socket = <path>`) with `chmod-socket = 666`. A per-account socket is, mechanically, just a different `http-socket` path (and, for isolation, a per-account uwsgi app + settings). No TCP port is ever opened.

## 3. The anonymity model fits anonctl EXACTLY (the important nuance)

webveil has TWO egress hops, and `egress.ts` enforces a fail-loud guard that lines up perfectly with the anonctl model:

- **`egress` (the BACKEND hop → SearXNG): MUST be `direct` for a `unix:` baseUrl.** `assertEgressAllowsBaseUrl()` THROWS if a `unix:` (or loopback) baseUrl is combined with a non-direct egress, because proxying an inherently-local hop is fake anonymity: "SearXNG still crawls the web from your real IP". So `egress: direct` is not a shortcut, it is REQUIRED for a socket backend.
- **`fetchEgress` (the `web_fetch` hop → arbitrary public URLs): the operator proxies this** (`socks5h://127.0.0.1:1080` in the live config). This is webveil's own comment-blessed "local-SearXNG + proxied-web_fetch" topology (its ADR-0003).

Under **anonctl**, this is even simpler than webveil's own proxying: anonctl forces the whole ACCOUNT's egress through the proxy at the kernel (per-UID), so BOTH SearXNG's crawl AND webveil's `web_fetch` are anonymized by the environment regardless of webveil's own `fetchEgress`. i.e. inside an anonctl-forced account, `egress: direct` + `fetchEgress: direct` is correct, because the kernel is doing the forcing. This mirrors anon-pi's netcage note ("SearXNG's crawl is anonymized because the environment forces every process's egress"), transposed from netns to per-UID.

## 4. Consequences for anonseed / the pi seed

- **A socket-wired SearXNG needs NO `--allow` exemption.** A Unix socket has no IP/port and never leaves the host; there is nothing for anonctl's nftables egress rule to match. (Contrast the AI MODEL, which is TCP and DOES need `--allow IP:port`.) So this is purely a config-seeding + service-presence question, not an exemption question.
- **The seed config anonseed would write is tiny and known:** a `~/.config/webveil/config.json` with `backend: searxng`, `baseUrl: unix:<per-account-socket>`, `egress: direct` (required), and `fetchEgress: direct` (because anonctl forces egress). That is 4 keys.
- **Two seedable shapes** (matching the idea note): (A) point webveil at a CONFIGURABLE existing socket path (default `unix:/usr/local/searxng/run/socket`) — pure config write, zero lifecycle; (B) a PER-ACCOUNT embedded SearXNG on its own socket — same tiny config, but adds the "make sure SearXNG is running on that socket" lifecycle.

## Open (NOT yet verified by running it)

- **Per-account multi-instance feasibility (shape B).** Running a SECOND, account-scoped SearXNG bound to a DIFFERENT socket path (its own uwsgi app + `SEARXNG_SETTINGS_PATH` + `secret_key`), owned by the anon account, launched/kept-running by SOMETHING. The socket mechanics are trivially per-path; the open part is the LIFECYCLE owner (the seed? a systemd unit the seed drops? out of anonseed's scope, documented?). This is the Q2(ii) "does the seed interface stage/launch a service" fork. Not proven here; would need a Phase-2 prototype AND explicit permission to run a second SearXNG.
- Shape (A) has NO open feasibility risk: it is exactly what the operator runs today, minus the per-account isolation.
