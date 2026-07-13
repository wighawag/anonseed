package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// piOnPath is the production pi-presence seam: it reports whether the `pi` binary
// is reachable on PATH. The seed writes pi's CONFIG, so a missing pi means the
// seeded identity has nothing to run that config; the handler warns (in red) when
// this is false. Behind the handler's piPresent seam so tests force present/absent
// without depending on the host's PATH.
func piOnPath() bool {
	_, err := exec.LookPath("pi")
	return err == nil
}

// resolvePiSeed is the production pi-seed resolution seam: it runs piseed.Resolve
// (probe the endpoint's live /v1/models, read the endpoint-matched provider from
// the user's own ~/.pi/agent/models.json, run the api-key guard) with the real
// impure edges wired, then wraps the resolved Options in a piseed.Seed. A refused
// real apiKey (or any resolution failure) is returned as an error, aborting the
// seed before any target work. force carries the operator's explicit
// --force-allow-local-llm-api-key through to the guard.
func resolvePiSeed(ctx context.Context, endpoint string, force bool, webveil piseed.WebveilChoice, stdout, _ io.Writer) (seed.Seed, error) {
	fmt.Fprintf(stdout, "anonseed pi: probing model endpoint %s ...\n", endpoint)
	opts, err := piseed.Resolve(ctx, piseed.ResolveInput{
		Endpoint:       endpoint,
		Force:          force,
		Probe:          httpProbe,
		ReadUserModels: readUserModels,
		Pick:           interactiveDefaultPick,
		DetectSearxng:  interactiveSearxngDetect(webveil),
		Webveil:        webveil,
	})
	if err != nil {
		return nil, err
	}

	// Verbose progress: report what was resolved so the operator sees what the seed
	// decided (which models, whether webveil got wired and at which socket) rather
	// than a single silent "seeded" line.
	fmt.Fprintf(stdout, "anonseed pi: %d model(s) selected (default: %s)\n", len(opts.Models), opts.DefaultModelID)
	if opts.Webveil != nil {
		fmt.Fprintf(stdout, "anonseed pi: webveil web search wired at SearXNG socket %s (declaring the %s extension)\n",
			opts.Webveil.SocketPath, piseed.WebveilPackage)
	} else if webveil.Disabled {
		fmt.Fprintln(stdout, "anonseed pi: webveil disabled (--no-webveil); seeding model config only")
	} else {
		fmt.Fprintln(stdout, "anonseed pi: no SearXNG detected; webveil not wired (pass --webveil-install-default to wire it anyway)")
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

// interactiveDefaultPick is the production model pick: it imports EVERY discovered
// candidate (the seed always wires every endpoint-served model) but ASKS the
// operator WHICH is the default rather than auto-picking the first. The prompt is
// skipped when there is nothing to choose: 0 candidates (model-only, no pick) or 1
// candidate (it is trivially the default). For 2+ it lists them numbered and reads
// a 1-based choice on stdin, defaulting to the first (ID-sorted) on an empty line.
func interactiveDefaultPick(candidates []piseed.Candidate) (piseed.Pick, error) {
	return defaultPickFrom(os.Stdin, os.Stderr, candidates)
}

// defaultPickFrom is interactiveDefaultPick's testable core: it reads the choice
// from in and writes the prompt to out, so a test drives it with a string reader
// and a buffer (no real stdin/stderr). See interactiveDefaultPick for behaviour.
func defaultPickFrom(in io.Reader, out io.Writer, candidates []piseed.Candidate) (piseed.Pick, error) {
	if len(candidates) == 0 {
		return piseed.Pick{}, nil
	}
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	if len(ids) == 1 {
		return piseed.Pick{ImportIDs: ids, DefaultID: ids[0]}, nil
	}

	fmt.Fprintln(out, "Models to import (all are wired); choose the DEFAULT:")
	for i, id := range ids {
		fmt.Fprintf(out, "  %d) %s\n", i+1, id)
	}
	fmt.Fprintf(out, "Default model [1-%d] (empty = 1, %s): ", len(ids), ids[0])

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return piseed.Pick{}, err
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		return piseed.Pick{ImportIDs: ids, DefaultID: ids[0]}, nil
	}
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(ids) {
		return piseed.Pick{}, fmt.Errorf("invalid default-model choice %q (want a number 1-%d)", choice, len(ids))
	}
	return piseed.Pick{ImportIDs: ids, DefaultID: ids[n-1]}, nil
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

// searxngInstallURL is the SearXNG install guide the not-found prompt points at.
// It targets the BARE-METAL (uWSGI) step-by-step install, NOT the generic index
// (which also offers Docker). anonseed detects SearXNG by reading the host uWSGI
// app ini's `http-socket` and wires webveil over that Unix SOCKET, so a Docker /
// container install is NOT usable here: it exposes no host uWSGI socket to detect
// or bind. The bare install is the one that produces the socket this seed needs.
const searxngInstallURL = "https://docs.searxng.org/admin/installation-searxng.html"

// interactiveSearxngDetect wraps the raw host detector with the not-found UX: when
// SearXNG is NOT detected (and webveil is neither disabled nor pointed at an
// explicit socket, in which cases the answer is already settled), it ASKS the
// operator what to do rather than silently disabling webveil. It points at the
// install guide and offers a RECHECK so the operator can install SearXNG in
// another shell and continue without restarting the seed.
//
// The three choices map onto the SearxngDetection the pure ResolveWebveil then
// consumes:
//   - [p]roceed: return absent -> webveil disabled (the model-only fallback).
//   - [r]echeck: re-run the raw detector and loop (the operator just installed it).
//   - [i]nstall-default: return Present with the install-default socket, so
//     ResolveWebveil wires webveil there (the operator will provide a SearXNG at
//     that socket) -- equivalent to --webveil-install-default.
//
// It short-circuits (no prompt) when the operator already settled webveil via a
// flag: Disabled or a SocketPathOverride both bypass detection in ResolveWebveil,
// and a successful detection needs no prompt either.
func interactiveSearxngDetect(choice piseed.WebveilChoice) piseed.DetectSearxngFunc {
	return func() (piseed.SearxngDetection, error) {
		return searxngDetectFrom(os.Stdin, os.Stderr, detectHostSearxng, choice)
	}
}

// searxngDetectFrom is interactiveSearxngDetect's testable core: it runs the given
// detect seam and, on an absent-and-unsettled result, drives the not-found prompt
// loop reading from in / writing to out. A test injects a scripted reader, a
// buffer, and a fake detect (present/absent, with a recheck that flips to
// present), so the whole decision tree runs without a real host or real stdin.
func searxngDetectFrom(in io.Reader, out io.Writer, detect func() (piseed.SearxngDetection, error), choice piseed.WebveilChoice) (piseed.SearxngDetection, error) {
	det, err := detect()
	if err != nil {
		det = piseed.SearxngDetection{Present: false}
	}
	// Already-settled cases skip the prompt: a detected SearXNG, an explicit
	// disable, an explicit socket override, or the flag that pre-accepts the
	// install default. Only an ABSENT SearXNG with no such flag reaches the prompt.
	if det.Present || choice.Disabled ||
		strings.TrimSpace(choice.SocketPathOverride) != "" ||
		choice.AcceptInstallDefaultWhenAbsent {
		return det, nil
	}

	reader := bufio.NewReader(in)
	for {
		fmt.Fprintln(out, "No SearXNG detected on this host; webveil web search needs one.")
		fmt.Fprintln(out, "Install SearXNG BARE-METAL (uWSGI), NOT Docker: the seed wires webveil over a host Unix socket, which a container does not expose.")
		fmt.Fprintf(out, "Guide: %s\n", searxngInstallURL)
		fmt.Fprintf(out, "[p]roceed without webveil, [r]echeck after installing, [i]nstall-default (wire anyway, you'll provide one) [p]: ")

		line, rerr := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if rerr != nil && ans == "" {
			// No input available (EOF/non-interactive): take the safe default (proceed
			// without webveil) rather than blocking.
			return piseed.SearxngDetection{Present: false}, nil
		}
		switch ans {
		case "", "p", "proceed":
			return piseed.SearxngDetection{Present: false}, nil
		case "i", "install-default":
			// Wire at the install default: report Present with an empty socket so
			// ResolveWebveil uses DefaultSearxngSocketPath.
			return piseed.SearxngDetection{Present: true}, nil
		case "r", "recheck":
			if redet, derr := detect(); derr == nil && redet.Present {
				fmt.Fprintf(out, "SearXNG now detected%s.\n", socketSuffix(redet.SocketPath))
				return redet, nil
			}
			fmt.Fprintln(out, "Still no SearXNG detected.")
			continue
		default:
			fmt.Fprintf(out, "Unrecognised choice %q.\n", ans)
			continue
		}
	}
}

// socketSuffix renders a " at <path>" clause for a non-empty socket path, or "" so
// the recheck confirmation reads naturally when the path is unknown.
func socketSuffix(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return " at " + path
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

// interactiveEndpointPrompt is the production endpoint prompt: it asks the operator
// (on stdin) for the local model endpoint host:port when --endpoint was omitted, so
// the pi seed is usable interactively. It reads one line and returns it trimmed;
// the handler treats an empty answer as a usage error. Behind the handler's
// endpointPrompt seam, so cli tests script the answer without real stdin.
func interactiveEndpointPrompt() (string, error) {
	fmt.Fprintf(os.Stderr, "Local model endpoint (host:port) the seeded pi reaches directly: ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// interactiveOverwritePrompt is the production overwrite prompt: ON a create-only
// collision (the seed would clobber files the home already has), it lists the
// colliding paths and asks the operator whether to overwrite, defaulting to NO on
// an empty line or EOF (create-only is the safe default; the operator must opt in
// to clobbering). Behind the handler's overwritePrompt seam, so cli tests script
// the y/N answer without real stdin. Passing --overwrite bypasses this entirely
// (the policy pre-authorises with no prompt).
func interactiveOverwritePrompt(paths []string) (bool, error) {
	return overwritePromptFrom(os.Stdin, os.Stderr, paths)
}

// overwritePromptFrom is interactiveOverwritePrompt's testable core: it lists the
// colliding paths to out and reads a y/N answer from in, so a test drives it with
// a string reader and a buffer (no real stdin/stderr). An empty answer or EOF is
// NO (the create-only default), so a non-interactive run never silently clobbers.
func overwritePromptFrom(in io.Reader, out io.Writer, paths []string) (bool, error) {
	fmt.Fprintf(out, "anonseed pi: %d file(s) already exist in the target home:\n", len(paths))
	for _, p := range paths {
		fmt.Fprintf(out, "  - %s\n", p)
	}
	fmt.Fprintf(out, "Overwrite them? (default: no, keeps existing) [y/N]: ")

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	if err != nil && ans == "" {
		// No input available (EOF / non-interactive): take the safe default (do NOT
		// overwrite) rather than blocking or clobbering.
		return false, nil
	}
	switch ans {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
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
