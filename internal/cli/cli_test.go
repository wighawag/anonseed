package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/wighawag/anonseed/internal/elevate"
)

// fakeHandler is a trivial in-test seed handler used to exercise the CLI dispatch
// + self-elevation seams WITHOUT the real pi handler's impure edges (a live
// endpoint probe, a stdin prompt, an /etc/anonctl write). It echoes a marker to
// stdout and returns a fixed code, and records the args it was handed.
type fakeHandler struct {
	code    int
	marker  string
	gotArgs *[]string
}

func (h fakeHandler) Summary() string { return "fake seed for dispatch tests" }

func (h fakeHandler) Run(args []string, stdout, _ io.Writer) int {
	if h.gotArgs != nil {
		*h.gotArgs = append([]string(nil), args...)
	}
	if h.marker != "" {
		io.WriteString(stdout, h.marker)
	}
	return h.code
}

// stubRegistry replaces the CLI's built-in registry with a single fake `pi`
// handler, so dispatch tests route to a controllable handler rather than the real
// pi seed. The original is restored on cleanup.
func stubRegistry(t *testing.T, h Handler) {
	t.Helper()
	orig := defaultRegistry
	defaultRegistry = func() registry { return registry{"pi": h} }
	t.Cleanup(func() { defaultRegistry = orig })
}

// stubElevate replaces the CLI's self-elevation seam with a fixed Decision so seed
// dispatch is exercised WITHOUT a real sudo/root re-exec. It also records the argv
// the gate was asked to elevate, so tests can assert argv is forwarded exactly.
// The original is restored on cleanup.
func stubElevate(t *testing.T, dec elevate.Decision) *[]string {
	t.Helper()
	var gotArgv []string
	orig := ensureElevated
	ensureElevated = func(argv []string, _ io.Writer) elevate.Decision {
		gotArgv = append([]string(nil), argv...)
		return dec
	}
	t.Cleanup(func() { ensureElevated = orig })
	return &gotArgv
}

// TestRunDispatch exercises the CLI/handler seam: known seed names route to
// their handler, unknown names fail loudly, and the meta flags behave.
func TestRunDispatch(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantCode     int
		wantStdoutIn string // substring expected on stdout ("" == no check)
		wantStderrIn string // substring expected on stderr ("" == no check)
	}{
		{
			name:         "known seed pi routes to its handler",
			args:         []string{"pi"},
			wantCode:     0,
			wantStdoutIn: "FAKE-PI",
		},
		{
			name:         "known seed pi forwards its own args",
			args:         []string{"pi", "--endpoint", "127.0.0.1:1234"},
			wantCode:     0,
			wantStdoutIn: "FAKE-PI",
		},
		{
			name:         "unknown seed fails loudly and lists seeds",
			args:         []string{"nope"},
			wantCode:     2,
			wantStderrIn: "unknown seed type",
		},
		{
			name:         "unknown seed names the offending token",
			args:         []string{"nope"},
			wantCode:     2,
			wantStderrIn: `"nope"`,
		},
		{
			name:         "help on stdout, clean exit",
			args:         []string{"--help"},
			wantCode:     0,
			wantStdoutIn: "Usage:",
		},
		{
			name:         "version prints a version",
			args:         []string{"--version"},
			wantCode:     0,
			wantStdoutIn: "anonseed ",
		},
		{
			name:         "no args shows help as a usage error",
			args:         nil,
			wantCode:     2,
			wantStderrIn: "Usage:",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Neutralise the self-elevation gate so a seed dispatch runs its handler
			// in-process (as if already privileged); the gate itself is covered below.
			stubElevate(t, elevate.Decision{AlreadyPrivileged: true})
			// Route `pi` to a controllable fake so dispatch is tested without the real
			// pi handler's impure edges.
			stubRegistry(t, fakeHandler{code: 0, marker: "FAKE-PI"})
			var stdout, stderr bytes.Buffer
			got := Run(tc.args, &stdout, &stderr)

			if got != tc.wantCode {
				t.Errorf("Run(%q) exit code = %d, want %d", tc.args, got, tc.wantCode)
			}
			if tc.wantStdoutIn != "" && !strings.Contains(stdout.String(), tc.wantStdoutIn) {
				t.Errorf("Run(%q) stdout = %q, want it to contain %q", tc.args, stdout.String(), tc.wantStdoutIn)
			}
			if tc.wantStderrIn != "" && !strings.Contains(stderr.String(), tc.wantStderrIn) {
				t.Errorf("Run(%q) stderr = %q, want it to contain %q", tc.args, stderr.String(), tc.wantStderrIn)
			}
		})
	}
}

// TestUnknownSeedListsAvailable checks the unknown-seed error (the reserved
// PATH-plugin seam) is helpful: it lists the built-in seeds so the user can
// recover.
func TestUnknownSeedListsAvailable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	Run([]string{"definitely-not-a-seed"}, &stdout, &stderr)

	for name := range defaultRegistry() {
		if !strings.Contains(stderr.String(), name) {
			t.Errorf("unknown-seed error should list built-in seed %q; stderr = %q", name, stderr.String())
		}
	}
}

// TestSeedDispatchSelfElevates proves the wiring: dispatching a recognised seed
// (a root-requiring /etc/anonctl write) goes through the self-elevation gate,
// forwarding the FULL argv (seed name + its args) so the elevated child re-runs the
// identical command.
func TestSeedDispatchSelfElevates(t *testing.T) {
	gotArgv := stubElevate(t, elevate.Decision{AlreadyPrivileged: true})
	stubRegistry(t, fakeHandler{code: 0, marker: "FAKE-PI"})
	var stdout, stderr bytes.Buffer
	Run([]string{"pi", "--target", "anonctl"}, &stdout, &stderr)

	want := []string{"pi", "--target", "anonctl"}
	if strings.Join(*gotArgv, " ") != strings.Join(want, " ") {
		t.Errorf("elevation argv = %q, want %q (the full argv must be forwarded for re-exec)", *gotArgv, want)
	}
}

// TestSeedDispatchReexecReturnsChildCodeWithoutRunningHandler proves that when the
// gate re-execs (non-root), the handler does NOT run again in this process and the
// child's exit code propagates verbatim.
func TestSeedDispatchReexecReturnsChildCodeWithoutRunningHandler(t *testing.T) {
	stubElevate(t, elevate.Decision{Reexeced: true, ExitCode: 7})
	var stdout, stderr bytes.Buffer
	code := Run([]string{"pi"}, &stdout, &stderr)

	if code != 7 {
		t.Errorf("exit code = %d, want 7 (the elevated child's code must propagate)", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("handler ran in-process after a re-exec (stdout = %q); it must not run twice", stdout.String())
	}
}

// TestSeedDispatchElevationUnavailableFailsLoud proves elevation-unavailable is a
// LOUD failure at dispatch: a non-zero exit and a clear stderr message, and the
// handler never runs (so nothing is half-written).
func TestSeedDispatchElevationUnavailableFailsLoud(t *testing.T) {
	stubElevate(t, elevate.Decision{Err: errors.New("needs root but sudo not found on PATH")})
	var stdout, stderr bytes.Buffer
	code := Run([]string{"pi"}, &stdout, &stderr)

	if code == 0 {
		t.Errorf("elevation-unavailable must exit non-zero, got 0")
	}
	if !strings.Contains(stderr.String(), "needs root") {
		t.Errorf("elevation-unavailable stderr = %q, want a clear message", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("handler ran despite unavailable elevation (stdout = %q); it must not (no partial write)", stdout.String())
	}
}

// TestVersionOverridable documents that --version reports the package version
// var (build-time -X override point), not a hard-coded literal.
func TestVersionOverridable(t *testing.T) {
	orig := version
	t.Cleanup(func() { version = orig })

	version = "v9.9.9-test"
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--version"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("--version exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "v9.9.9-test") {
		t.Errorf("--version stdout = %q, want it to contain the overridden version", stdout.String())
	}
}
