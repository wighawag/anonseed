// Package cli implements the anonseed command-line surface: argv parsing and
// dispatch of the first positional argument to a per-seed-type handler.
//
// This is the tracer-bullet skeleton (see work/tasks: bootstrap-go-module-and-cli-skeleton).
// It stands up the family conventions only:
//
//   - one binary, per-seed-type subcommands: `anonseed <seed> ...`
//   - a registry keyed by seed name (see registry.go), so `pi` routes to a stub;
//   - a loud, helpful error for an unknown subcommand. That error site is the
//     seam the RESERVED PATH-plugin fallback (`anonseed foo` -> exec PATH
//     `anonseed-foo`) will hook into later. The fallback itself is NOT built
//     here (it is speculative until a third-party seed exists; see CONTEXT.md
//     "PATH-plugin (reserved)").
//
// The actual seed logic (config synthesis, the --allow declaration, the api-key
// guard) is intentionally absent: this file only proves the dispatch seam.
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/wighawag/anonseed/internal/elevate"
)

// ensureElevated is the self-elevation seam, injectable so cli tests drive seed
// dispatch WITHOUT a real sudo/root re-exec. It is called once a seed subcommand
// is dispatched (every built-in seed writes host state under /etc/anonctl, a
// root-owned path), BEFORE the handler runs: a non-root invocation re-execs the
// SAME argv elevated (mirroring anonctl), and elevation-unavailable is a loud
// failure surfaced here (never a silent skip or a partial write). Production wires
// elevate.Ensure; the elevate package holds the decision + argv-construction seam.
//
// The gate is at seed dispatch (not after --target parsing) because every built-in
// seed targets anonctl today and so needs root; the --target axis is a separate
// task. The loop-guard sentinel makes this safe: the elevated child re-enters Run
// with ANONSEED_ELEVATED set, so it does NOT re-elevate and proceeds to the handler
// as root. See work/notes/observations/self-elevation-decisions.md + docs/adr/0003.
var ensureElevated = func(argv []string, stderr io.Writer) elevate.Decision {
	return elevate.Ensure(context.Background(), argv, func(msg string) {
		fmt.Fprintln(stderr, msg)
	})
}

// version is the anonseed version string reported by `--version`.
//
// It defaults to a dev sentinel and is meant to be overridden at build time via
// the linker, e.g. `go build -ldflags "-X github.com/wighawag/anonseed/internal/cli.version=v0.1.0"`.
// A build-time -X override is chosen over embedding a constant or reading
// runtime/debug.BuildInfo so a real release can stamp a clean tag without a
// source edit, while a plain `go build` still yields a usable dev string.
var version = "dev"

// Run parses args (the program arguments, WITHOUT the argv[0] program name) and
// dispatches to the matching seed handler. It writes normal output to stdout and
// diagnostics to stderr, and returns a process exit code (0 == success).
//
// Run never calls os.Exit itself, so it is drivable from tests at the CLI/handler
// seam.
func Run(args []string, stdout, stderr io.Writer) int {
	reg := defaultRegistry()

	// No subcommand: show help. Treat it as a usage error (non-zero) so a bare
	// `anonseed` in a script is noticed, matching common CLI convention.
	if len(args) == 0 {
		printHelp(stderr, reg)
		return 2
	}

	switch args[0] {
	case "-h", "--help", "help":
		printHelp(stdout, reg)
		return 0
	case "-v", "--version", "version":
		fmt.Fprintf(stdout, "anonseed %s\n", version)
		return 0
	}

	name := args[0]
	rest := args[1:]

	handler, ok := reg[name]
	if !ok {
		// The unknown-seed seam. This loud error is where the reserved
		// PATH-plugin fallback will eventually hook in (exec `anonseed-<name>`);
		// for now it just fails helpfully.
		fmt.Fprintf(stderr, "anonseed: unknown seed type %q\n", name)
		fmt.Fprintf(stderr, "\nAvailable seed types:\n")
		printSeeds(stderr, reg)
		fmt.Fprintf(stderr, "\nRun 'anonseed --help' for usage.\n")
		return 2
	}

	// A recognised seed writes host state under /etc/anonctl (root-owned), so it is
	// a root-requiring step: self-elevate rather than print a "run as root" hint.
	// Ensure re-execs the SAME argv elevated if we are not root; if it re-exec'd we
	// return the child's exit code and do NOT run the handler again in this process;
	// if elevation is unavailable we fail LOUD here, before any write.
	switch dec := ensureElevated(args, stderr); {
	case dec.Err != nil:
		fmt.Fprintf(stderr, "anonseed: %v\n", dec.Err)
		return 1
	case dec.Reexeced:
		return dec.ExitCode
		// dec.AlreadyPrivileged: fall through and run the handler in-process.
	}

	return handler.Run(rest, stdout, stderr)
}
