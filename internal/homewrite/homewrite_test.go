package homewrite_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wighawag/anoncore/seedhome"
	"github.com/wighawag/anonseed/internal/homewrite"
	"github.com/wighawag/anonseed/internal/seed"
)

// fakeRunner records the chown calls seedhome issues so the tests assert account
// ownership WITHOUT a real chown (which needs root). The file copy itself is real
// I/O into a temp dir, so mode/collision/strip behaviour is exercised for real.
// This is the Runner seam the task requires: no root, no real chown.
type fakeRunner struct{ calls [][]string }

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return "", "", nil
}

// getentRunner scripts `getent passwd <account>` for ResolveAccountHome so the
// passwd lookup runs through the Runner seam with no real account on the box. Any
// non-getent call (a chown) is recorded but returns success.
type getentRunner struct {
	homes map[string]string // account -> home dir in the fake passwd
	calls [][]string
}

func (r *getentRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if name == "getent" && len(args) == 2 && args[0] == "passwd" {
		home, ok := r.homes[args[1]]
		if !ok {
			return "", "", nil // getent exits non-zero / empty when absent
		}
		// passwd line: name:x:uid:gid:gecos:home:shell
		return args[1] + ":x:1000:1000::" + home + ":/bin/bash\n", "", nil
	}
	return "", "", nil
}

// TestWriteLandsFiles is the happy path: a plan's files land under the resolved
// home with content preserved, and every written path is chowned to the account
// through the Runner seam (no real chown).
func TestWriteLandsFiles(t *testing.T) {
	home := t.TempDir()
	files := []seed.FileToWrite{
		{Path: ".pi/agent/models.json", Content: `{"providers":[]}`},
		{Path: ".pi/agent/settings.json", Content: `{"defaultModel":"x"}`},
	}
	r := &fakeRunner{}

	res, err := homewrite.Write(context.Background(), r, homewrite.Identity{Home: home, Account: "anon"}, files, false)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if res.Copied != 2 {
		t.Errorf("Copied = %d, want 2", res.Copied)
	}
	if got, err := os.ReadFile(filepath.Join(home, ".pi/agent/models.json")); err != nil || string(got) != `{"providers":[]}` {
		t.Errorf("models.json = %q, err %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(home, ".pi/agent/settings.json")); err != nil || string(got) != `{"defaultModel":"x"}` {
		t.Errorf("settings.json = %q, err %v", got, err)
	}
	// The chown went through the Runner seam and targeted the account, not root.
	if len(r.calls) == 0 {
		t.Fatal("expected chown calls through the Runner seam, got none")
	}
	for _, call := range r.calls {
		if call[0] != "chown" || call[1] != "anon:anon" {
			t.Errorf("unexpected Runner call %v, want a chown to anon:anon", call)
		}
	}
}

// TestWriteCollisionRefusesAtomically: create-only by default. An existing target
// file aborts the whole write (NOTHING is written) with a *seedhome.ErrCollision,
// and no chown runs. This is the load-bearing atomic collision guarantee.
func TestWriteCollisionRefusesAtomically(t *testing.T) {
	home := t.TempDir()
	// The home already has one of the files the plan would write.
	if err := os.MkdirAll(filepath.Join(home, ".pi/agent"), 0o700); err != nil {
		t.Fatalf("prep home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".pi/agent/models.json"), []byte("EXISTING"), 0o600); err != nil {
		t.Fatalf("prep existing file: %v", err)
	}
	files := []seed.FileToWrite{
		{Path: ".pi/agent/models.json", Content: "NEW"},
		{Path: ".pi/agent/fresh.json", Content: "FRESH"},
	}
	r := &fakeRunner{}

	_, err := homewrite.Write(context.Background(), r, homewrite.Identity{Home: home, Account: "anon"}, files, false)
	var ce *seedhome.ErrCollision
	if !errors.As(err, &ce) {
		t.Fatalf("expected *seedhome.ErrCollision, got %v", err)
	}
	// NOTHING written: the existing file is untouched and the fresh file was never
	// created (atomic abort before any copy).
	if got, _ := os.ReadFile(filepath.Join(home, ".pi/agent/models.json")); string(got) != "EXISTING" {
		t.Errorf("existing file was modified despite collision: %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".pi/agent/fresh.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("fresh file was created despite an aborted (atomic) write")
	}
	if len(r.calls) != 0 {
		t.Errorf("no chown should run on an aborted write, got %v", r.calls)
	}
}

// TestWriteForceOverwrites: an explicit force overwrites the colliding file (the
// only way past create-only), and seedhome records the overwrite.
func TestWriteForceOverwrites(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".pi/agent"), 0o700); err != nil {
		t.Fatalf("prep home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".pi/agent/models.json"), []byte("EXISTING"), 0o600); err != nil {
		t.Fatalf("prep existing file: %v", err)
	}
	files := []seed.FileToWrite{{Path: ".pi/agent/models.json", Content: "NEW"}}
	r := &fakeRunner{}

	res, err := homewrite.Write(context.Background(), r, homewrite.Identity{Home: home, Account: "anon"}, files, true)
	if err != nil {
		t.Fatalf("Write --force: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(home, ".pi/agent/models.json")); string(got) != "NEW" {
		t.Errorf("file not overwritten under force: %q", got)
	}
	if len(res.Overwrote) != 1 || res.Overwrote[0] != ".pi/agent/models.json" {
		t.Errorf("Overwrote = %v, want [.pi/agent/models.json]", res.Overwrote)
	}
}

// TestWriteStripsSetuid: files landed through this surface never carry setuid/
// setgid/sticky bits. The strip is anoncore seedhome's guarantee (it cannot drift
// because it is the same code anonctl uses); asserting it at anonseed's seam
// proves a seeded home can never gain a uid-transition-escape file.
func TestWriteStripsSetuid(t *testing.T) {
	home := t.TempDir()
	files := []seed.FileToWrite{{Path: ".pi/agent/models.json", Content: "x"}}
	r := &fakeRunner{}

	if _, err := homewrite.Write(context.Background(), r, homewrite.Identity{Home: home, Account: "anon"}, files, false); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(filepath.Join(home, ".pi/agent/models.json"))
	if err != nil {
		t.Fatalf("stat seeded file: %v", err)
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		t.Errorf("seeded file carries setuid/setgid/sticky: mode %v", info.Mode())
	}
}

// TestWritePlantedSetuidIsStripped drives the strip harder: even if a template
// file somehow carried the setuid bit, seedhome strips it on the way into the
// home. Because Write materialises then seeds in one call, this test reproduces
// the materialise+seed pipeline while planting a setuid file, proving the strip
// survives regardless of a file's source mode.
func TestWritePlantedSetuidIsStripped(t *testing.T) {
	tmpl := t.TempDir()
	home := t.TempDir()
	// A setuid file planted directly in the template dir Write would build.
	if err := os.WriteFile(filepath.Join(tmpl, "tool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write planted file: %v", err)
	}
	if err := os.Chmod(filepath.Join(tmpl, "tool"), os.ModeSetuid|0o755); err != nil {
		t.Fatalf("chmod setuid: %v", err)
	}
	r := &fakeRunner{}
	if _, err := seedhome.Seed(context.Background(), r, tmpl, home, "anon", false); err != nil {
		t.Fatalf("seed planted template: %v", err)
	}
	info, err := os.Stat(filepath.Join(home, "tool"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode()&os.ModeSetuid != 0 {
		t.Errorf("planted setuid bit was NOT stripped: mode %v", info.Mode())
	}
}

// TestResolveDefaultHome covers the FIRST sub-target: the box-wide default-home
// template under the (caller-supplied, temp) base dir, owned by the default anon
// account. No real /etc/anonctl is touched: the base dir is a temp path.
func TestResolveDefaultHome(t *testing.T) {
	base := t.TempDir()
	id := homewrite.ResolveDefaultHome(base)
	wantHome := filepath.Join(base, "default-home")
	if id.Home != wantHome {
		t.Errorf("Home = %q, want %q", id.Home, wantHome)
	}
	if id.Account != "anon" {
		t.Errorf("Account = %q, want anon (the default account)", id.Account)
	}
}

// TestResolveAccountHome covers the SECOND sub-target: a specific named account's
// home, resolved through anoncore's account vocabulary (`work` -> `anon-work`)
// and a passwd lookup over the Runner seam (a scripted fake getent, no real
// account). The bare/default name and an already-prefixed name are covered too.
func TestResolveAccountHome(t *testing.T) {
	base := t.TempDir()
	cases := []struct {
		name        string
		wantAccount string
	}{
		{"work", "anon-work"},
		{"", "anon"},
		{"anon", "anon"},
		{"anon-work", "anon-work"},
	}
	for _, tc := range cases {
		t.Run("name="+tc.name, func(t *testing.T) {
			wantHome := filepath.Join(base, tc.wantAccount)
			r := &getentRunner{homes: map[string]string{tc.wantAccount: wantHome}}
			id, err := homewrite.ResolveAccountHome(context.Background(), r, tc.name)
			if err != nil {
				t.Fatalf("ResolveAccountHome(%q): %v", tc.name, err)
			}
			if id.Account != tc.wantAccount {
				t.Errorf("Account = %q, want %q", id.Account, tc.wantAccount)
			}
			if id.Home != wantHome {
				t.Errorf("Home = %q, want %q", id.Home, wantHome)
			}
		})
	}
}

// TestResolveAccountHomeMissing: an account with no passwd home is a loud error,
// not a silent empty home (which would seed into a wrong/empty path).
func TestResolveAccountHomeMissing(t *testing.T) {
	r := &getentRunner{homes: map[string]string{}}
	if _, err := homewrite.ResolveAccountHome(context.Background(), r, "ghost"); err == nil {
		t.Fatal("expected an error for an account with no passwd home, got nil")
	}
}

// TestWriteRefusesPathTraversal: a SeedPlan declares home-RELATIVE paths; an
// absolute path or one that escapes via `..` is refused, so a plan can never land
// a file outside the resolved home.
func TestWriteRefusesPathTraversal(t *testing.T) {
	home := t.TempDir()
	r := &fakeRunner{}
	for _, bad := range []string{"/etc/passwd", "../escape", "../../etc/x", ""} {
		files := []seed.FileToWrite{{Path: bad, Content: "x"}}
		if _, err := homewrite.Write(context.Background(), r, homewrite.Identity{Home: home, Account: "anon"}, files, false); err == nil {
			t.Errorf("Write accepted an unsafe path %q, want a refusal", bad)
		}
	}
}

// TestWriteIsolation is the shared-write isolation guarantee: with the home
// pointed at a scratch dir and the chown behind a fake Runner, NO path outside
// the fixture is touched. We snapshot a sentinel outside the home and assert it is
// unchanged, and assert only chown (never useradd/rm/real fs mutation) reaches the
// Runner.
func TestWriteIsolation(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("prep home: %v", err)
	}
	// A sentinel OUTSIDE the home (a sibling), representing "the real filesystem".
	sentinel := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(sentinel, []byte("UNTOUCHED"), 0o600); err != nil {
		t.Fatalf("prep sentinel: %v", err)
	}

	files := []seed.FileToWrite{{Path: ".pi/agent/models.json", Content: "x"}}
	r := &fakeRunner{}
	if _, err := homewrite.Write(context.Background(), r, homewrite.Identity{Home: home, Account: "anon"}, files, false); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got, _ := os.ReadFile(sentinel); string(got) != "UNTOUCHED" {
		t.Errorf("a path OUTSIDE the home was modified: sentinel = %q", got)
	}
	// Every Runner call is a chown (the only impure op), never a real fs mutation.
	for _, call := range r.calls {
		if call[0] != "chown" {
			t.Errorf("Runner saw a non-chown op %v; the surface must only chown", call)
		}
	}
}
