package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wighawag/anoncore/provision"
	"github.com/wighawag/anonseed/internal/homewrite"
	"github.com/wighawag/anonseed/internal/piseed"
	"github.com/wighawag/anonseed/internal/seed"
)

// pi_production.go assembles the PRODUCTION impure edges the pi handler injects:
// the interactive seed resolution (probe + read user config + pick), the
// interactive target prompt, and the anoncore ExecRunner. They are kept here,
// out of the handler's own file, so seed_pi.go stays about the target-axis
// wiring and every one of these is behind the handler's seam (the cli tests
// substitute fakes and never touch a real endpoint, the real filesystem, or a
// real /etc/anonctl).
//
// NOTE on scope: the pi seed's interactive model-PICK here is deliberately a
// non-interactive "import every discovered model, first as default" default, NOT
// a rich TUI. The full interactive pick surface belongs to the pi seed's own CLI
// work (task pi-seed-model-config shipped the piseed Resolve/Plan library); this
// task owns the --target axis, so it wires piseed.Resolve through a simple pick
// rather than building the TUI. See the done record's Decisions block.
//
// webveil (task pi-seed-webveil-anonctl-socket) IS wired here: resolvePiSeed
// passes the operator's WebveilChoice + the host SearXNG detection seam
// (detectHostSearxng) through to piseed.Resolve, so webveil is default-on
// (disable-able) with the config landed by the target applier alongside the model
// files. Detection is a cheap host sniff (the uWSGI app ini's `http-socket`), kept
// behind piseed's DetectSearxngFunc seam so cli/piseed tests never read /etc.

// provisionExecRunner returns the real anoncore Runner for the anonctl applier's
// chown / passwd steps. Wrapped in a function so the handler wiring names one
// production seam rather than the concrete type.
func provisionExecRunner() homewrite.Runner { return provision.ExecRunner{} }

// resolvePiSeed is the production pi-seed resolution seam: it runs piseed.Resolve
// (probe the endpoint's live /v1/models, read the endpoint-matched provider from
// the user's own ~/.pi/agent/models.json, run the api-key guard) with the real
// impure edges wired, then wraps the resolved Options in a piseed.Seed. A refused
// real apiKey (or any resolution failure) is returned as an error, aborting the
// seed before any target work. force carries the operator's explicit
// --force-allow-local-llm-api-key through to the guard.
func resolvePiSeed(ctx context.Context, endpoint string, force bool, webveil piseed.WebveilChoice, _, _ io.Writer) (seed.Seed, error) {
	opts, err := piseed.Resolve(ctx, piseed.ResolveInput{
		Endpoint:       endpoint,
		Force:          force,
		Probe:          httpProbe,
		ReadUserModels: readUserModels,
		Pick:           importAllPick,
		DetectSearxng:  detectHostSearxng,
		Webveil:        webveil,
	})
	if err != nil {
		return nil, err
	}
	return piseed.New(opts), nil
}

// httpProbe fetches the endpoint's live /v1/models body over real HTTP. A probe
// error is NON-FATAL to Resolve (it falls back to the user-config models alone),
// so this returns the error for Resolve to swallow rather than aborting here.
func httpProbe(ctx context.Context, endpoint string) (json.RawMessage, error) {
	url := "http://" + endpoint + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("probe %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// readUserModels reads the operator's own ~/.pi/agent/models.json bytes. A
// not-found / unreadable file is NON-FATAL to Resolve (returns the error, which
// Resolve swallows to rely on the live probe alone).
func readUserModels() ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(home, ".pi", "agent", "models.json"))
}

// importAllPick is the non-interactive default pick: import EVERY discovered
// candidate, with the first (candidates are ID-sorted) as the default. A rich
// interactive pick is deferred to the pi seed's own CLI surface (see the file
// doc); this keeps the --target-axis wiring functional without a TUI.
func importAllPick(candidates []piseed.Candidate) (piseed.Pick, error) {
	if len(candidates) == 0 {
		return piseed.Pick{}, nil
	}
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	return piseed.Pick{ImportIDs: ids, DefaultID: ids[0]}, nil
}

// searxngUwsgiIniPaths are the host locations the production SearXNG detector
// reads to find an install + its socket, in preference order: the enabled uWSGI
// app symlink first (the served one), then the available definition. Each is the
// `.ini` whose `http-socket = <path>` line binds the SearXNG socket (see the
// finding webveil-searxng-unix-socket-contract.md). Presence of the file signals a
// SearXNG install; the `http-socket` line gives the socket path to wire.
var searxngUwsgiIniPaths = []string{
	"/etc/uwsgi/apps-enabled/searxng.ini",
	"/etc/uwsgi/apps-available/searxng.ini",
}

// detectHostSearxng is the production SearXNG-detection seam (piseed.DetectSearxngFunc):
// it reports whether a host SearXNG is installed and the socket its uWSGI app
// binds, by reading the SearXNG uWSGI app ini's `http-socket = <path>` line. It is
// a cheap, filesystem-only sniff (no exec, no network), matching the target
// detector's stance. When no ini is found, SearXNG is reported ABSENT (the seed
// then takes the disable-or-install-default branch); when found but the socket
// line is unreadable, it is reported PRESENT with an empty socket path (the
// resolution falls back to the install default). A read error other than
// not-found is returned so Resolve can treat it as "not detected" (non-fatal).
func detectHostSearxng() (piseed.SearxngDetection, error) {
	for _, path := range searxngUwsgiIniPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return piseed.SearxngDetection{}, err
		}
		// Found an install: parse its `http-socket = <path>` (best-effort; an
		// unreadable socket line still reports Present so the seed defers to the
		// install default rather than silently disabling webveil).
		return piseed.SearxngDetection{
			Present:    true,
			SocketPath: parseUwsgiHTTPSocket(data),
		}, nil
	}
	return piseed.SearxngDetection{Present: false}, nil
}

// parseUwsgiHTTPSocket extracts the socket path from a uWSGI ini's
// `http-socket = <path>` line (the line SearXNG's app config uses to bind its
// socket). It ignores comments (`#`) and whitespace and returns the first match's
// value, or "" when no such line is present (the caller falls back to the install
// default). It is a small line scan, not a full ini parser: only the one key the
// socket path lives on is needed.
func parseUwsgiHTTPSocket(data []byte) string {
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) != "http-socket" {
			continue
		}
		if v := strings.TrimSpace(value); v != "" {
			return v
		}
	}
	return ""
}

// interactiveTargetPrompt is the production detect-then-ask prompt: given the
// substrates detected PRESENT, it asks the operator (on stdin) which to seed, so
// the default path NEVER silently auto-picks. It reads a comma-separated choice
// (or "all" / empty for none) from stdin. The whole prompt is only reached on the
// no-`--target` default path; an explicit --target bypasses it entirely.
func interactiveTargetPrompt(present []seed.Target) ([]seed.Target, error) {
	fmt.Fprintf(os.Stderr, "Detected substrate(s): %s\n", joinTargetNames(present))
	fmt.Fprintf(os.Stderr, "Which to seed? (comma-separated names, 'all', or empty for none): ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}
	if strings.EqualFold(line, "all") {
		return present, nil
	}

	presentSet := make(map[string]seed.Target, len(present))
	for _, t := range present {
		presentSet[string(t)] = t
	}
	var chosen []seed.Target
	for _, tok := range strings.Split(line, ",") {
		name := strings.TrimSpace(tok)
		if name == "" {
			continue
		}
		t, ok := presentSet[name]
		if !ok {
			return nil, fmt.Errorf("%q is not among the detected substrates (%s)", name, joinTargetNames(present))
		}
		chosen = append(chosen, t)
	}
	return chosen, nil
}

// joinTargetNames renders a target set as a comma-separated list for prompt text.
func joinTargetNames(targets []seed.Target) string {
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, string(t))
	}
	return strings.Join(names, ", ")
}
