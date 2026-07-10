// Package elevate is anonseed's self-elevation seam for the operations that must
// write host state under `/etc/anonctl` (root-owned). Mirroring anonctl's stance
// (../anonctl/elevate.go), a non-privileged invocation that REACHES a
// root-requiring step re-executes the SAME anonseed command through `sudo` so the
// operator gets the password prompt INLINE in the terminal (a bare
// `anonseed pi --target anonctl` works, no `sudo anonseed` prefix needed).
// anonseed then DOES the privileged work itself rather than printing
// paste-these-commands.
//
// The mechanism is deliberately anonctl's, so the two tools behave identically:
//
//   - sudo, NOT pkexec: on a tty sudo prompts in-terminal, whereas pkexec pops a
//     polkit GUI dialog the family avoids.
//   - re-exec THIS binary via os.Executable (/proc/self/exe on Linux), so the
//     child is the same anonseed, preserving argv EXACTLY (flags/target/subcommand).
//   - a belt-and-suspenders loop-guard sentinel env: set on the child before
//     re-exec; if already set on entry we never re-exec (combined with the euid!=0
//     gate this makes a re-exec loop impossible even under a misconfigured sudo).
//   - the impure steps (geteuid, sudo lookup, self path, re-exec) are behind
//     package-var seams so tests drive the decision + argv construction WITHOUT a
//     real sudo, a real re-exec, or a password prompt.
//
// WHERE anonseed DIVERGES from anonctl, deliberately (see docs/adr/0003):
//
//   - Elevation is keyed on REACHING A ROOT-REQUIRING STEP (the caller invokes
//     Ensure at the point it is about to write under /etc/anonctl), NOT on a
//     dispatch-time table of root-requiring verbs. anonseed's need-for-root is
//     TARGET-dependent (writing the anonctl substrate needs root; a plan-only /
//     dry run does not), so the decision lives at the step, not at argv dispatch.
//   - sudo-unavailable is a LOUD, HARD FAILURE here, not a fall-through. anonctl
//     falls through to the verb's own "must be root" error; anonseed has no such
//     downstream error at this seam, and the task requires "elevation unavailable
//     = a loud, clear failure (never a silent skip or a partial write)". So
//     Ensure returns an error the caller surfaces and exits non-zero on, BEFORE
//     any /etc/anonctl write happens.
//   - No anonctl-session refusal branch: that guards anonctl's `use`-dropped anon
//     shell (an account with no sudo), a concept anonseed does not have.
package elevate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// SentinelEnv is the loop-guard environment variable. Before re-exec anonseed sets
// it in the child's environment; if it is ALREADY set on entry, anonseed never
// re-execs (it would loop). Combined with the euid!=0 gate (after sudo the child
// is euid 0, so it would not re-fire anyway), this makes a re-exec loop impossible
// even under a misconfigured sudo that failed to actually elevate.
const SentinelEnv = "ANONSEED_ELEVATED"

// ErrSudoNotFound is returned when `sudo` is not on PATH. Unlike anonctl (which
// falls through to a verb's own "must be root" error), anonseed treats this as a
// LOUD failure at the seam, because the task requires elevation-unavailable to be
// a clear failure, never a silent skip or a partial /etc/anonctl write.
var ErrSudoNotFound = errors.New("sudo not found on PATH")

// Seams: package vars so the unit tests drive the elevation decision + re-exec
// argv construction WITHOUT a real sudo, a real re-exec, or a password prompt.
// Production wires the real os.Geteuid / exec.LookPath / os.Executable /
// exec-and-wait.
var (
	// geteuid reports the effective UID (os.Geteuid in production).
	geteuid = os.Geteuid

	// lookSudo resolves `sudo` on PATH, or returns ErrSudoNotFound.
	lookSudo = func() (string, error) {
		p, err := exec.LookPath("sudo")
		if err != nil {
			return "", ErrSudoNotFound
		}
		return p, nil
	}

	// selfPath resolves the path to THIS anonseed binary to re-exec (os.Executable,
	// i.e. /proc/self/exe on Linux), so the child is the same binary.
	selfPath = os.Executable

	// reexec runs `sudo <self> <args...>` with the given env, waits, and returns
	// the child's exit code (the value anonseed exits with, so a failing seed still
	// exits non-zero and any gating stays intact).
	reexec = runElevated
)

// Decision is the outcome of the elevation check for a reached root-requiring step.
// Exactly one of three things is true:
//
//   - AlreadyPrivileged: the process is euid 0 (or the loop-guard sentinel is set
//     after a prior elevation). The caller PROCEEDS to do the privileged work
//     directly in-process; no re-exec.
//   - Reexeced: a re-exec via sudo ran to completion. The caller must NOT do the
//     work again in this process; it exits with ExitCode (the child's code).
//   - neither, with Err set: elevation was REQUIRED but is UNAVAILABLE (no sudo,
//     or self-path unresolved). This is a LOUD failure; the caller surfaces Err
//     and exits non-zero WITHOUT writing anything.
type Decision struct {
	// AlreadyPrivileged is true when the current process may do the privileged work
	// itself (euid 0, or re-entered post-elevation via the sentinel). The caller
	// proceeds.
	AlreadyPrivileged bool

	// Reexeced is true when a sudo re-exec ran; ExitCode carries the child's exit
	// code. The caller must return ExitCode and do NO further work.
	Reexeced bool

	// ExitCode is the child's exit code when Reexeced is true (undefined otherwise).
	ExitCode int

	// Err is non-nil ONLY when elevation was required but unavailable (loud
	// failure). It is nil for both AlreadyPrivileged and Reexeced.
	Err error
}

// Ensure is called at the point anonseed REACHES a root-requiring step (about to
// write under /etc/anonctl) and decides how to obtain the needed privilege. It
// returns a Decision the caller acts on:
//
//   - Decision.AlreadyPrivileged: proceed to do the privileged work in-process.
//   - Decision.Reexeced: return Decision.ExitCode; the work already ran (or failed)
//     in the elevated child. Do NOT repeat it.
//   - Decision.Err != nil: elevation was required but is UNAVAILABLE; surface the
//     error and exit non-zero WITHOUT writing anything (never a partial write).
//
// argv is the FULL argument vector to re-run (WITHOUT the program name), i.e. what
// the anonseed entrypoint received (os.Args[1:]); it is preserved exactly so the
// elevated child runs the identical command. notice is written to stderr before a
// re-exec so the coming sudo password prompt is not a surprise; pass nil to suppress.
//
// Ensure never prompts or execs in a way a test can observe a password: the prompt
// is sudo's, and every impure step is behind a seam the unit tests replace.
func Ensure(ctx context.Context, argv []string, notice func(string)) Decision {
	if geteuid() == 0 {
		return Decision{AlreadyPrivileged: true} // already root: do the work directly
	}
	if os.Getenv(SentinelEnv) != "" {
		// Loop guard: we already tried to elevate (or a parent set the sentinel) and
		// are STILL not root. Do not re-exec (that would loop). Treat as "proceed":
		// the caller's privileged write will fail with its own clear permission error
		// if it truly still lacks privilege, but we never recurse.
		return Decision{AlreadyPrivileged: true}
	}

	sudoPath, err := lookSudo()
	if err != nil {
		// No sudo on PATH. LOUD failure (divergence from anonctl's fall-through):
		// anonseed must not silently skip or half-write, so we return the error for
		// the caller to surface and exit non-zero on, BEFORE any /etc/anonctl write.
		return Decision{Err: fmt.Errorf("this operation must write under /etc/anonctl and needs root, but %w; re-run as root (e.g. `sudo anonseed ...`)", err)}
	}

	self, err := selfPath()
	if err != nil {
		// Cannot resolve our own path to re-exec. Also a loud failure rather than a
		// guess at a PATH name that might be wrong.
		return Decision{Err: fmt.Errorf("this operation needs root but anonseed could not resolve its own executable path to re-run elevated: %w; re-run as root (e.g. `sudo anonseed ...`)", err)}
	}

	if notice != nil {
		notice("anonseed: this step needs root; re-running via sudo...")
	}

	// argv: sudo <self> <original args...>, preserving the original args exactly.
	full := append([]string{sudoPath, self}, argv...)
	env := append(os.Environ(), SentinelEnv+"=1")
	return Decision{Reexeced: true, ExitCode: reexec(ctx, sudoPath, full, env)}
}

// runElevated is the production re-exec: run `sudo <self> <args...>` inheriting the
// terminal (so sudo prompts inline and the elevated seed's I/O reaches the
// operator), wait, and return the child's exit code EXACTLY (so a failing seed
// still exits non-zero). A launch failure (sudo vanished between lookup and exec)
// maps to exit 1.
func runElevated(ctx context.Context, _ string, argv, env []string) int {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "anonseed: re-exec via sudo failed: %v\n", err)
		return 1
	}
	return 0
}
