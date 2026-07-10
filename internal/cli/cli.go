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
	"fmt"
	"io"
)

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

	return handler.Run(rest, stdout, stderr)
}
