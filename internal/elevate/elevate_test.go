package elevate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// recordedReexec captures what a re-exec was asked to run, so a test can assert the
// argv construction WITHOUT a real sudo, a real exec, or a password prompt.
type recordedReexec struct {
	sudoPath string
	argv     []string
	env      []string
}

// swapSeams installs fake elevation seams and returns a pointer to the recorded
// re-exec (nil until a re-exec is attempted). euid is the simulated effective UID;
// sudoPresent decides whether `sudo` resolves; selfErr, when non-nil, simulates a
// self-path resolution failure; childExit is the exit code the fake re-exec returns.
// All originals are restored on test cleanup.
func swapSeams(t *testing.T, euid int, sudoPresent bool, selfErr error, childExit int) **recordedReexec {
	t.Helper()
	var rec *recordedReexec
	origEuid, origLook, origSelf, origReexec := geteuid, lookSudo, selfPath, reexec
	geteuid = func() int { return euid }
	lookSudo = func() (string, error) {
		if sudoPresent {
			return "/usr/bin/sudo", nil
		}
		return "", ErrSudoNotFound
	}
	selfPath = func() (string, error) {
		if selfErr != nil {
			return "", selfErr
		}
		return "/opt/anonseed/anonseed", nil
	}
	reexec = func(_ context.Context, sudoPath string, argv, env []string) int {
		rec = &recordedReexec{sudoPath: sudoPath, argv: argv, env: env}
		return childExit
	}
	t.Cleanup(func() {
		geteuid, lookSudo, selfPath, reexec = origEuid, origLook, origSelf, origReexec
	})
	return &rec
}

// NEEDS-ELEVATION: a non-root process reaching a root-requiring step re-execs
// `sudo <self> <original argv>`, preserving argv EXACTLY, sets the loop-guard
// sentinel in the child env, emits the stderr notice, and propagates the child's
// exit code verbatim.
func TestEnsureNonRootReexecsPreservingArgv(t *testing.T) {
	recp := swapSeams(t, 1000, true, nil, 7)

	var notice string
	argv := []string{"pi", "--target", "anonctl", "--endpoint", "127.0.0.1:1234"}
	dec := Ensure(context.Background(), argv, func(s string) { notice = s })

	if dec.AlreadyPrivileged {
		t.Errorf("non-root reached a root step but Ensure reported AlreadyPrivileged")
	}
	if dec.Err != nil {
		t.Fatalf("unexpected Err on a normal elevate path: %v", dec.Err)
	}
	if !dec.Reexeced {
		t.Fatalf("non-root did not re-exec via sudo")
	}
	rec := *recp
	if rec == nil {
		t.Fatalf("re-exec seam was not invoked")
	}
	want := []string{"/usr/bin/sudo", "/opt/anonseed/anonseed", "pi", "--target", "anonctl", "--endpoint", "127.0.0.1:1234"}
	if strings.Join(rec.argv, " ") != strings.Join(want, " ") {
		t.Errorf("re-exec argv = %q, want %q", rec.argv, want)
	}
	if dec.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7 (the child's exit must propagate exactly)", dec.ExitCode)
	}
	// The loop-guard sentinel must be set in the CHILD env so a failed elevation
	// cannot recurse.
	if !hasEnv(rec.env, SentinelEnv+"=1") {
		t.Errorf("child env missing loop-guard sentinel %s=1; env = %v", SentinelEnv, rec.env)
	}
	if !strings.Contains(notice, "needs root") {
		t.Errorf("notice = %q, want it to mention needing root", notice)
	}
}

// ALREADY-ROOT: euid 0 does the work IN-PROCESS. No re-exec (no double-sudo).
func TestEnsureAlreadyRootDoesNotReexec(t *testing.T) {
	recp := swapSeams(t, 0, true, nil, 0)

	dec := Ensure(context.Background(), []string{"pi", "--target", "anonctl"}, nil)

	if !dec.AlreadyPrivileged {
		t.Errorf("euid 0 should be AlreadyPrivileged (proceed in-process)")
	}
	if dec.Reexeced {
		t.Errorf("already-root re-exec'd via sudo; must run directly (no double-sudo)")
	}
	if dec.Err != nil {
		t.Errorf("already-root produced an error: %v", dec.Err)
	}
	if *recp != nil {
		t.Errorf("already-root invoked the re-exec seam; it must not")
	}
}

// ELEVATION-UNAVAILABLE (no sudo): a LOUD failure. Ensure returns an error and does
// NOT re-exec and does NOT claim AlreadyPrivileged, so the caller surfaces the
// error and exits non-zero BEFORE any /etc/anonctl write (never a silent skip or a
// partial write). This is anonseed's deliberate divergence from anonctl's
// fall-through.
func TestEnsureSudoMissingIsLoudFailure(t *testing.T) {
	recp := swapSeams(t, 1000, false, nil, 0)

	dec := Ensure(context.Background(), []string{"pi", "--target", "anonctl"}, nil)

	if dec.Err == nil {
		t.Fatalf("sudo absent must be a loud failure (non-nil Err), got none")
	}
	if !errors.Is(dec.Err, ErrSudoNotFound) {
		t.Errorf("Err = %v, want it to wrap ErrSudoNotFound", dec.Err)
	}
	if !strings.Contains(dec.Err.Error(), "/etc/anonctl") {
		t.Errorf("Err should name the /etc/anonctl privileged write; got %q", dec.Err.Error())
	}
	if dec.Reexeced {
		t.Errorf("sudo absent must NOT re-exec")
	}
	if dec.AlreadyPrivileged {
		t.Errorf("sudo absent must NOT report AlreadyPrivileged (that would silently proceed unprivileged)")
	}
	if *recp != nil {
		t.Errorf("sudo absent invoked the re-exec seam; it must not")
	}
}

// ELEVATION-UNAVAILABLE (self-path unresolved): also a loud failure rather than a
// guess at a PATH name that might be wrong.
func TestEnsureSelfPathUnresolvedIsLoudFailure(t *testing.T) {
	recp := swapSeams(t, 1000, true, errors.New("boom"), 0)

	dec := Ensure(context.Background(), []string{"pi", "--target", "anonctl"}, nil)

	if dec.Err == nil {
		t.Fatalf("unresolved self path must be a loud failure (non-nil Err), got none")
	}
	if dec.Reexeced || dec.AlreadyPrivileged {
		t.Errorf("unresolved self path must neither re-exec nor claim privilege; dec = %+v", dec)
	}
	if *recp != nil {
		t.Errorf("unresolved self path invoked the re-exec seam; it must not")
	}
}

// LOOP GUARD: with the sentinel already set, a non-root process does NOT re-exec
// (it would loop). It reports AlreadyPrivileged so the caller proceeds and the
// privileged write surfaces its own permission error if truly still unprivileged,
// rather than recursing under a misconfigured sudo.
func TestEnsureLoopGuardBlocksReexecWhenSentinelSet(t *testing.T) {
	t.Setenv(SentinelEnv, "1")
	recp := swapSeams(t, 1000, true, nil, 0)

	dec := Ensure(context.Background(), []string{"pi", "--target", "anonctl"}, nil)

	if dec.Reexeced {
		t.Errorf("re-exec fired with %s already set; the loop guard must block it", SentinelEnv)
	}
	if !dec.AlreadyPrivileged {
		t.Errorf("with the sentinel set, Ensure should report AlreadyPrivileged (proceed, do not recurse)")
	}
	if *recp != nil {
		t.Errorf("loop guard: the re-exec seam must not be invoked")
	}
}

// hasEnv reports whether entry appears verbatim in the env slice.
func hasEnv(env []string, entry string) bool {
	for _, e := range env {
		if e == entry {
			return true
		}
	}
	return false
}
