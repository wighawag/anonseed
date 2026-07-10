package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// searxngIniFixture is a minimal uWSGI app ini mirroring the real SearXNG one
// (the lines that matter: the http-socket path, plus noise to prove the scan
// skips comments and other keys).
const searxngIniFixture = `# -*- mode: conf -*-
[uwsgi]
uid = searxng
# http-socket = /commented/out/should/be/ignored
chdir = /usr/local/searxng/searxng-src/searx
env = SEARXNG_SETTINGS_PATH=/etc/searxng/settings.yml
http-socket = /usr/local/searxng/run/socket
buffer-size = 8192
`

// TestParseUwsgiHTTPSocket reads the socket path from the http-socket line,
// skipping comments and other keys.
func TestParseUwsgiHTTPSocket(t *testing.T) {
	got := parseUwsgiHTTPSocket([]byte(searxngIniFixture))
	if got != "/usr/local/searxng/run/socket" {
		t.Errorf("parseUwsgiHTTPSocket = %q, want /usr/local/searxng/run/socket", got)
	}
	// A commented-out http-socket alone yields no path.
	if v := parseUwsgiHTTPSocket([]byte("# http-socket = /x\nuid = searxng\n")); v != "" {
		t.Errorf("commented http-socket should not match, got %q", v)
	}
	// No http-socket line at all -> empty (caller falls back to the install default).
	if v := parseUwsgiHTTPSocket([]byte("[uwsgi]\nuid = searxng\n")); v != "" {
		t.Errorf("absent http-socket should yield empty, got %q", v)
	}
}

// withSearxngIniPaths swaps the detector's ini search paths to the given fixture
// paths for the duration of a test (shared-write isolation: the detector reads
// only temp fixtures, never the real /etc/uwsgi).
func withSearxngIniPaths(t *testing.T, paths []string) {
	t.Helper()
	orig := searxngUwsgiIniPaths
	searxngUwsgiIniPaths = paths
	t.Cleanup(func() { searxngUwsgiIniPaths = orig })
}

// TestDetectHostSearxngPresent: an ini fixture present -> detected, with the
// socket path read from it. It writes and reads only under a temp dir.
func TestDetectHostSearxngPresent(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "searxng.ini")
	if err := os.WriteFile(iniPath, []byte(searxngIniFixture), 0o600); err != nil {
		t.Fatalf("write fixture ini: %v", err)
	}
	withSearxngIniPaths(t, []string{filepath.Join(dir, "missing.ini"), iniPath})

	det, err := detectHostSearxng()
	if err != nil {
		t.Fatalf("detectHostSearxng: %v", err)
	}
	if !det.Present {
		t.Fatal("SearXNG should be detected when the ini fixture exists")
	}
	if det.SocketPath != "/usr/local/searxng/run/socket" {
		t.Errorf("socket = %q, want the ini's http-socket path", det.SocketPath)
	}
}

// TestDetectHostSearxngAbsent: no ini anywhere -> not detected (the seed then
// takes the disable-or-install-default branch). Points the search paths at a temp
// dir with no ini, so the real host is never read.
func TestDetectHostSearxngAbsent(t *testing.T) {
	dir := t.TempDir()
	withSearxngIniPaths(t, []string{
		filepath.Join(dir, "a.ini"),
		filepath.Join(dir, "b.ini"),
	})
	det, err := detectHostSearxng()
	if err != nil {
		t.Fatalf("detectHostSearxng: %v", err)
	}
	if det.Present {
		t.Errorf("SearXNG should be absent when no ini exists, got %+v", det)
	}
}

// TestDetectHostSearxngPresentNoSocketLine: an ini present but with NO http-socket
// line -> detected with an empty socket path (the resolution then falls back to
// the install default rather than silently disabling webveil).
func TestDetectHostSearxngPresentNoSocketLine(t *testing.T) {
	dir := t.TempDir()
	iniPath := filepath.Join(dir, "searxng.ini")
	if err := os.WriteFile(iniPath, []byte("[uwsgi]\nuid = searxng\n"), 0o600); err != nil {
		t.Fatalf("write fixture ini: %v", err)
	}
	withSearxngIniPaths(t, []string{iniPath})

	det, err := detectHostSearxng()
	if err != nil {
		t.Fatalf("detectHostSearxng: %v", err)
	}
	if !det.Present {
		t.Error("an ini present (even without http-socket) should report Present")
	}
	if det.SocketPath != "" {
		t.Errorf("no http-socket line should yield an empty socket path, got %q", det.SocketPath)
	}
}
