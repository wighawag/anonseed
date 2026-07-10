package cli

import (
	"bytes"
	"strings"
	"testing"
)

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
			name:         "known seed pi routes to its stub",
			args:         []string{"pi"},
			wantCode:     0,
			wantStdoutIn: "anonseed pi",
		},
		{
			name:         "known seed pi forwards its own args",
			args:         []string{"pi", "--endpoint", "127.0.0.1:1234"},
			wantCode:     0,
			wantStdoutIn: "not yet implemented",
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
