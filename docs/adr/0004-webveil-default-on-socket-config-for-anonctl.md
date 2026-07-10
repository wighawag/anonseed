# webveil is a default-on part of the pi seed, wired at a SearXNG Unix socket

The pi seed wires webveil (anonymized web search over SearXNG) BY DEFAULT for the anonctl target, disable-able during seeding. This is the ONE deliberate exception to the seed's otherwise strict "no arbitrary extensions / no other provider" rule (prd stories 22, 22a-22e; CONTEXT.md webveil): an agent that cannot search the web is crippled, and webveil specifically carries no new leak (SearXNG runs locally and anonctl forces the account's egress at the kernel, so both SearXNG's crawl and webveil's `web_fetch` are anonymized by the environment). The seed emits a webveil `config.json` alongside the model files; it does NOT install SearXNG and does NOT write/supervise the SearXNG service unit (that lifecycle is anonctl/anoncore's, see `work/notes/findings/per-account-service-lifecycle-solved-by-anonctl-shim-unit.md`).

The load-bearing shape decisions, pinned here so a later artifact does not silently re-derive them:

## The config is a Unix-socket baseUrl with direct/direct egress (no `--allow`)

Under anonctl, webveil reaches SearXNG over a Unix SOCKET, so the seed writes a `unix:<socketPath>` baseUrl (webveil's `core/baseurl.ts` grammar, verified live). The config is the four-key shape `{backend: "searxng", baseUrl: "unix:...", egress: {mode: "direct"}, fetchEgress: {mode: "direct"}}` (`internal/piseed` `GenerateWebveilConfig`), mirroring the operator's live `~/.config/webveil/config.json` transposed to the anonctl model:

- `egress: direct` is REQUIRED, not a shortcut: webveil's `core/egress.ts` `assertEgressAllowsBaseUrl` THROWS if a `unix:` baseUrl is combined with a non-direct egress (proxying an inherently-local hop is fake anonymity, because SearXNG still crawls the web from the real IP outside webveil's egress).
- `fetchEgress: direct` because anonctl forces the WHOLE account's egress per-UID at the kernel, so webveil's own `web_fetch` proxying is redundant (contrast the operator's live config, which uses `fetchEgress: socks5` because THEY are not inside an anonctl-forced account).
- A Unix socket has no IP/port and never leaves the host, so the webveil config declares NO `--allow` exception (nothing for anonctl's nftables egress rule to match). The pi seed's ONLY exception stays the model endpoint. This is the whole reason the socket path was chosen over an `IP:port` SearXNG (`work/notes/ideas/searxng-socket-wired-seed.md`).

## The config lands at the XDG default path, NOT under `.pi/`

webveil resolves its global config XDG-style (`$XDG_CONFIG_HOME/webveil/config.json`, falling back to `<home>/.config/webveil/config.json`, verified in webveil's `core/config.ts` `resolveGlobalPath`). The seed writes the config at the home-relative path `.config/webveil/config.json` (`piseed.WebveilConfigPath`), a DIFFERENT home subtree than the model files under `.pi/agent/`.

**Decision: write the `.config/` default, NOT a target-account `XDG_CONFIG_HOME` override.** `XDG_CONFIG_HOME` is the TARGET account's runtime environment, unknown at seed time (it is not the seeding operator's env, and reading the operator's would be wrong). anonctl homes are standard, so `.config/webveil/config.json` is the correct, only sensible home-relative target. This touches the file layout (a user-visible path), so it is recorded rather than buried. Alternative considered: probing/setting the target account's `XDG_CONFIG_HOME`, rejected as unknowable and unnecessary (the seeded home is standard). If a future substrate needs a non-standard config root, the applier (not the pure Plan) is where an override would land.

## The seed-time decision tree: default-on, disable-able, explicit fallback

The interactive `Resolve` (`internal/piseed`) detects a host SearXNG behind the `DetectSearxngFunc` seam and applies the operator's `WebveilChoice` through the PURE `ResolveWebveil`, yielding a `*WebveilOptions` (non-nil = webveil wired; nil = explicit model-only fallback). The tree, webveil default-ON:

1. Operator DISABLED webveil (`--no-webveil`) -> model-only. The one way to opt out of the default.
2. An explicit socket override (`--searxng-socket <path>`) -> wire at that socket (e.g. a known per-account instance).
3. A host SearXNG DETECTED -> wire at its socket (read from the uWSGI app ini's `http-socket` line, or the install default when unreadable).
4. NO SearXNG detected: an EXPLICIT choice, never silent. `--webveil-install-default` wires at the install-default socket (the operator will provide SearXNG there); otherwise the explicit model-only fallback (webveil off), CHOSEN, not silently defaulted-into.

**New CLI concepts introduced (recorded for coherence).** Three new pi-seed flags: `--no-webveil` (the disable), `--searxng-socket` (the socket override), `--webveil-install-default` (accept the install default when SearXNG is absent). They are pi-seed-local (they sit on the `anonseed pi` subcommand, not the shared surface) and do not overlap the `--allow`/`--force-allow-local-llm-api-key` concepts: webveil declares NO `--allow` (socket), and the api-key guard is orthogonal. Detection is a cheap filesystem-only host sniff (the uWSGI ini), matching `target.EnvDetector`'s no-exec stance; it never reads the real `/etc` in tests (the search-path list is a swappable package var, and the seam is faked in `piseed` tests).

## Considered / rejected

- **Wire webveil at an `IP:port` SearXNG with a second `--allow` hole.** Rejected: a socket needs no exemption and the anonymity of an `IP:port` SearXNG would become the user's responsibility (they must have wired it to a proxy). The socket path is cleaner and the anonctl-forced egress already anonymizes SearXNG's crawl. (`searxng-socket-wired-seed.md` option (a) vs (b).)
- **Install / supervise SearXNG from the seed.** Out of scope by the prd: SearXNG's install method changes upstream (anonseed would chase it), and the per-account service is anonctl/anoncore's lifecycle. anonseed DETECTS + guides + writes the config only.
- **webveil OFF by default (opt-in).** Rejected: this is the deliberate boundary reversal recorded in `searxng-socket-wired-seed.md` (an agent that cannot search is crippled; webveil is a known quantity with no new leak). Default-on with an explicit disable is the pinned stance.
- **anonbox target here.** Not this task: on anonbox, webveil rides the staged IMAGE (SearXNG baked in, already running) per prd story 22b; the anonbox applier records that intent (`internal/anonbox`). This task is the anonctl socket path (story 22c).
