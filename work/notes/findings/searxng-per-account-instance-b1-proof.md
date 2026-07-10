---
title: A per-account SearXNG worker reusing the shared install (B1) is proven to work
slug: searxng-per-account-instance-b1-proof
type: finding
source: |
  Phase-2 spike, RUN on this host 2026-07-10 (throwaway, cleaned up afterwards):
  a second uwsgi SearXNG worker started against the shared install
  (/usr/local/searxng/searx-pyenv + searxng-src) with its own socket, settings,
  and TMPDIR, then curled over the socket. Ground-truth from actually running it,
  not inference. The spike/ dir + /tmp socket were deleted after capture.
  Supporting host facts read read-only: /etc/uwsgi/apps-available/searxng.ini,
  /etc/init.d/uwsgi (globs /etc/uwsgi/apps-enabled/*.ini), /etc/searxng/settings.yml
  (use_default_settings: true, limiter: false), searx/cache.py (cache db path).
---

# B1 proof: a per-account SearXNG worker on its own socket, reusing the shared install

Answers the shape-B lifecycle question in `work/notes/ideas/searxng-socket-wired-seed.md`. B1 = "run an extra uwsgi worker of the ALREADY-INSTALLED SearXNG, per account, on its own socket." Proven end to end.

## What was run (and worked)

A throwaway uwsgi worker launched from a per-instance ini:

- `virtualenv = /usr/local/searxng/searx-pyenv` + `pythonpath = /usr/local/searxng/searxng-src` + `module = searx.webapp` \u2014 i.e. the SHARED, world-readable install, NOT a reinstall.
- `http-socket = <per-instance>.sock` \u2014 its own socket, no TCP port.
- `env = SEARXNG_SETTINGS_PATH=<per-instance>/settings.yml` \u2014 its own settings (own `secret_key`, `use_default_settings: true` to inherit everything else).
- `env = TMPDIR=<per-instance>/cache` \u2014 its own writable cache dir (see the gotcha below).

Result: socket bound (owned by the launching uid), `GET /` -> HTTP 200, `GET /search?q=hello+world&format=json` -> **HTTP 200 with 33 results**.

## The non-obvious per-instance requirement this surfaced (the spike's payoff)

SearXNG keeps a WRITABLE per-process cache in SQLite (`sxng_cache_DATA_CACHE.db`, `sxng_cache_ENGINES_CACHE.db`). Its default path is the TEMP DIR (`searx/cache.py`: `tempfile.gettempdir()/sxng_cache_{name}.db`). The FIRST run failed HTTP 500 (`sqlite3.OperationalError: attempt to write a readonly database`) because the shared install's `searxng` user already owns `/tmp/sxng_cache_*.db`, and a second worker under a different uid cannot write it. Fix: give each instance its OWN writable cache location (a per-instance `TMPDIR`, or a configured cache path). With that, the second run went green.

**Implication for anonseed:** a per-account SearXNG needs THREE isolated per-account things, not just a socket: (1) socket path, (2) settings file (own `secret_key`), (3) a WRITABLE cache dir. All three sit naturally inside the account's mode-700 home. The socket + settings were obvious from config; the writable-cache requirement was NOT, and would have bitten a config-only design.

## What is shared vs per-account (the reuse answer)

| Shared (read-only, one copy) | Per-account (isolated) |
| --- | --- |
| SearXNG code (`searxng-src`) | socket path (`http-socket`) |
| Python venv (`searx-pyenv`) | settings file + `secret_key` (`SEARXNG_SETTINGS_PATH`) |
| the uwsgi binary + plugins | writable cache dir (`TMPDIR` / cache path) |
| | uid/gid (run AS the anon account) |

No valkey/redis needed per instance: `limiter: false` in the host settings, and the limiter is the only valkey consumer. (If a future instance turned the limiter on, it would need a valkey \u2014 out of scope here.)

## Lifecycle owner (the fork this was meant to resolve)

The uwsgi orchestration on this host is the Debian sysv `uwsgi` init script globbing `/etc/uwsgi/apps-enabled/*.ini` (a symlink farm), NOT the emperor. So B1's lifecycle is literally: drop a per-account `<name>.ini` into `apps-enabled/` (with `uid=anon-<name>`, its socket, its `SEARXNG_SETTINGS_PATH`, its TMPDIR) and reload uwsgi. That is host service state, though \u2014 writing into `/etc/uwsgi` is beyond a strictly-declarative home-seeding step, so it REOPENS the prd's Q2(ii) pin (does the seed stage/launch a service). See the idea note for the B0-B3 landscape and which owner to pick.

## Honest caveats

- Run as MY uid against a `/tmp` socket, not as a real provisioned anon account. The uid/gid + mode-700-home placement is reasoned, not run. The core mechanics (shared code, independent socket, isolated cache, live search) ARE proven.
- Egress was direct in the spike (host `outgoing.proxies` inherited via `use_default_settings`), so results came back over the host's configured socks proxy. Under a real anonctl account, the kernel forces egress per-UID, so a per-account instance's crawl is anonymized by the environment regardless.
- The Debian-sysv-uwsgi lifecycle is THIS host's install method; another host may use the emperor or a systemd unit. That install-method variance is exactly the fragility that motivates B3 (account-owned SearXNG via a container image) as the no-host-dependency alternative.
