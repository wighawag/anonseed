// webveil.go holds the pi seed's webveil (anonymized web search) half: the PURE
// synthesis of a webveil `config.json` for the anonctl target, plus the SearXNG
// detection SEAM the interactive half drives.
//
// webveil is wired BY DEFAULT (an agent that cannot search the web is crippled),
// disable-able during seeding. It is the ONE deliberate extension exception in an
// otherwise no-arbitrary-extensions seed: it is a known quantity and carries no
// new leak, because SearXNG runs locally and anonctl forces the account's egress
// at the kernel. See CONTEXT.md (webveil) + work/notes/ideas/searxng-socket-wired-seed.md.
//
// # The anonctl webveil contract (proven; see the findings)
//
// Under anonctl, webveil talks to SearXNG over a Unix SOCKET, so it needs NO
// `--allow` exception (a socket has no IP/port and never leaves the host; there is
// nothing for anonctl's nftables egress rule to match). The config is tiny and
// known (mirrors the operator's live ~/.config/webveil/config.json, transposed to
// the anonctl model):
//
//   - backend:     "searxng"
//   - baseUrl:     "unix:<socketPath>" (the uWSGI Unix-domain-socket form webveil
//     parses; the backend then requests /search on it)
//   - egress:      {mode: "direct"}  REQUIRED for a socket backend: webveil's
//     egress guard THROWS if a unix: baseUrl is combined with a
//     non-direct egress (proxying an inherently-local hop is fake
//     anonymity). See webveil core/egress.ts assertEgressAllowsBaseUrl.
//   - fetchEgress: {mode: "direct"}  because anonctl forces the WHOLE account's
//     egress per-UID at the kernel, so BOTH SearXNG's crawl AND
//     webveil's web_fetch are anonymized by the environment, not by
//     webveil's own proxying.
//
// # The XDG config path (a DIFFERENT subtree than the model files)
//
// webveil resolves its global config XDG-style: $XDG_CONFIG_HOME/webveil/config.json,
// falling back to <home>/.config/webveil/config.json (verified in webveil
// core/config.ts resolveGlobalPath). The pi seed writes the config at the
// home-relative default `.config/webveil/config.json` — a SEPARATE subtree from the
// model files under `.pi/agent/` (the seed must NOT place the webveil config under
// `.pi/`). The seed writes the default location, not an XDG_CONFIG_HOME override:
// the target ACCOUNT's XDG_CONFIG_HOME is unknown at seed time (it is the target's
// runtime env, not the seeding operator's), and anonctl homes are standard, so the
// `.config/` default is the correct, only sensible home-relative target. See the
// task's done record Decisions block.
package piseed

import (
	"encoding/json"
	"strings"
)

// DefaultSearxngSocketPath is the install-default SearXNG uWSGI socket path, the
// shape (A)/(B0) shared-socket default (matching the operator's live setup and the
// finding webveil-searxng-unix-socket-contract.md). Detection may report a
// different path (a per-account instance's socket, or a non-standard install);
// this is the fallback used when the operator confirms webveil without a detected
// path.
const DefaultSearxngSocketPath = "/usr/local/searxng/run/socket"

// WebveilPackage is the pi extension source that provides webveil web search. When
// the seed wires webveil it declares this in the seeded settings.json `packages`
// so pi materialises the extension on first run (a DECLARATIVE install; the seed
// never execs `pi install`). Writing the .config/webveil/config.json alone is not
// enough: without the extension declared, pi has no webveil to read that config.
const WebveilPackage = "npm:pi-webveil"

// WebveilConfigPath is the home-relative path the pi seed writes webveil's config
// to: `.config/webveil/config.json`, the XDG default (see the file doc). It is a
// DIFFERENT subtree than the model files (ModelsFilePath / SettingsFilePath under
// `.pi/agent/`), deliberately NOT under `.pi/`.
const WebveilConfigPath = ".config/webveil/config.json"

// webveilBackend is the backend webveil speaks to: SearXNG.
const webveilBackend = "searxng"

// webveilEgress is one of webveil's two egress hops in the config the seed writes.
// The seed emits `{mode: "direct"}` for BOTH hops under anonctl (see the file doc).
type webveilEgress struct {
	Mode string `json:"mode"`
}

// webveilConfig is the exact on-disk shape webveil's resolveConfig consumes for
// the anonctl socket topology: backend + a unix: baseUrl + both egress hops
// direct. It mirrors the live ~/.config/webveil/config.json shape (minus the
// operator's socks5 fetchEgress, which anonctl replaces with kernel-forced
// egress, so the seed writes direct/direct). Only these four keys are written;
// webveil fills every other field (fetchSize, ...) from its own defaults.
type webveilConfig struct {
	Backend     string        `json:"backend"`
	BaseURL     string        `json:"baseUrl"`
	Egress      webveilEgress `json:"egress"`
	FetchEgress webveilEgress `json:"fetchEgress"`
}

// unixBaseURL builds webveil's Unix-domain-socket baseUrl form (`unix:<socketPath>`)
// from a socket path. webveil's baseurl.ts parses this into a socket-bound
// undici Agent scoped to the backend hop; the install default socket needs no
// httpPath suffix (the backend requests /search on `/`). A blank path yields the
// install default, so a degenerate detection never emits a malformed `unix:`.
func unixBaseURL(socketPath string) string {
	if socketPath == "" {
		socketPath = DefaultSearxngSocketPath
	}
	return "unix:" + socketPath
}

// GenerateWebveilConfig synthesises the webveil config.json for the anonctl target
// pointed at the SearXNG at socketPath: backend searxng, a unix: baseUrl, and both
// egress hops direct (see the file doc for why direct/direct is REQUIRED under
// anonctl). It is PURE: a deterministic function of the socket path, no I/O.
//
// It declares NO `--allow` exception (a socket has no IP/port; the caller adds no
// Exception for webveil). The output is tab-indented JSON with a trailing newline,
// matching the repo's on-disk style (marshalIndent, the same the model files use).
func GenerateWebveilConfig(socketPath string) ([]byte, error) {
	cfg := webveilConfig{
		Backend:     webveilBackend,
		BaseURL:     unixBaseURL(socketPath),
		Egress:      webveilEgress{Mode: "direct"},
		FetchEgress: webveilEgress{Mode: "direct"},
	}
	return marshalIndent(cfg)
}

// ensure the webveil config round-trips as the shape webveil reads (a compile-time
// witness that the struct tags match webveil's resolveConfig keys). Unused at
// runtime; kept as documentation of the contract.
var _ = func() bool {
	var c webveilConfig
	_ = json.Unmarshal([]byte(`{"backend":"","baseUrl":"","egress":{"mode":""},"fetchEgress":{"mode":""}}`), &c)
	return true
}

// SearxngDetection is what the detection seam reports about a host SearXNG install:
// whether one is present, and (if so) the socket path its settings bind. It drives
// the seed-time decision tree (see ResolveWebveil). A detector reads cheap host
// signals (the uWSGI app ini's `http-socket = <path>`, the settings file, the live
// socket); the detection itself is the impure edge (behind the DetectSearxngFunc
// seam), while the DECISION logic (ResolveWebveil) is pure.
type SearxngDetection struct {
	// Present is true when a host SearXNG install was detected. False means no
	// SearXNG: the decision tree falls to the disable-or-install-default choice.
	Present bool

	// SocketPath is the SearXNG uWSGI socket path the detected install binds (from
	// its `http-socket = <path>` app config), when known. Empty (even with
	// Present) means "detected, but the socket path could not be read": the
	// resolution falls back to DefaultSearxngSocketPath.
	SocketPath string
}

// DetectSearxngFunc reports whether a host SearXNG is installed and, if so, the
// socket its settings bind. It is the INTERACTIVE half's environment-sniffing
// seam: production reads the real host (the uWSGI app ini / settings / live
// socket); tests inject a fixture so present/absent + the socket path are
// deterministic and no test reads the real /etc/uwsgi or /etc/searxng. A
// detection error is returned so the caller can treat it as "not detected"
// (non-fatal) rather than aborting the seed.
type DetectSearxngFunc func() (SearxngDetection, error)

// WebveilChoice is the operator's seed-time webveil decision, the resolved output
// of the decision tree's interactivity. It exists so the DECISION (which the
// operator drives) is separated from the SYNTHESIS (ResolveWebveil turning it into
// Options): the CLI resolves this from a flag/prompt, ResolveWebveil applies it.
type WebveilChoice struct {
	// Disabled is the explicit operator DISABLE: when true, webveil is NOT wired
	// (the model-only fallback), regardless of detection. It is the disable
	// flag/prompt: webveil is default-ON, so this is the one way to turn it off.
	Disabled bool

	// SocketPathOverride, when non-empty, is an operator-supplied SearXNG socket
	// path that OVERRIDES both detection and the install default (e.g. wiring a
	// known per-account instance's socket the detector did not surface). Empty
	// defers to detection, then the install default.
	SocketPathOverride string

	// AcceptInstallDefaultWhenAbsent governs the NO-SearXNG-detected branch: when a
	// host SearXNG is NOT detected, wiring webveil at the install-default socket is
	// only meaningful if the operator intends to provide one. true wires webveil at
	// the default socket anyway (the operator will install/run SearXNG there);
	// false takes the explicit model-only fallback (webveil off). It is IGNORED
	// when SearXNG IS detected (detection wins) or SocketPathOverride is set.
	AcceptInstallDefaultWhenAbsent bool
}

// ResolveWebveil applies the seed-time webveil decision tree, returning the
// resolved *WebveilOptions to place on piseed.Options: non-nil when webveil is
// WIRED (default-on), nil for the explicit model-only fallback. It is the seam
// where detection + the operator's choice meet, kept PURE (the impure detection is
// the passed detection value, resolved upstream): a deterministic function of
// (detection, choice).
//
// The tree (mirrors work/notes/ideas/searxng-socket-wired-seed.md), webveil
// default-on:
//
//  1. Operator DISABLED webveil (choice.Disabled) -> nil (model-only). The
//     disable flag/prompt is the one way to turn off the default.
//  2. An operator socket OVERRIDE is set -> wire at that socket (they named a
//     specific SearXNG, e.g. a per-account instance).
//  3. A host SearXNG is DETECTED -> wire at its socket (the detected path, or the
//     install default when the path could not be read).
//  4. NO SearXNG detected: an EXPLICIT choice, never silent. If the operator
//     accepts the install default (AcceptInstallDefaultWhenAbsent) -> wire at the
//     default socket (they will provide SearXNG there); else -> nil, the explicit
//     model-only fallback (webveil off), chosen, not defaulted-into silently.
//
// Detection wins over AcceptInstallDefaultWhenAbsent (that flag governs only the
// absent branch); an explicit SocketPathOverride wins over both.
func ResolveWebveil(detection SearxngDetection, choice WebveilChoice) *WebveilOptions {
	if choice.Disabled {
		return nil // (1) explicit disable: the model-only fallback.
	}
	if path := strings.TrimSpace(choice.SocketPathOverride); path != "" {
		return &WebveilOptions{SocketPath: path} // (2) operator-named socket.
	}
	if detection.Present {
		// (3) detected: wire at the detected socket, or the install default when the
		// path could not be read.
		path := strings.TrimSpace(detection.SocketPath)
		if path == "" {
			path = DefaultSearxngSocketPath
		}
		return &WebveilOptions{SocketPath: path}
	}
	// (4) not detected: an explicit choice.
	if choice.AcceptInstallDefaultWhenAbsent {
		return &WebveilOptions{SocketPath: DefaultSearxngSocketPath}
	}
	return nil // explicit model-only fallback (never silent).
}
