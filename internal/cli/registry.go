package cli

import (
	"fmt"
	"io"
	"sort"
)

// Handler is the behaviour of one built-in seed type. Each seed registered in
// the registry implements Run, which receives the arguments AFTER the seed name
// (e.g. for `anonseed pi --endpoint ...`, Run gets `["--endpoint", "..."]`) and
// returns a process exit code.
//
// This is deliberately a tiny interface: the tracer bullet only needs the
// dispatch seam. The richer seed contract (resolve the target home, write files
// via anoncore seedhome, declare the --allow exception, enforce the api-key
// guard) is future work and will grow around, not replace, this seam.
type Handler interface {
	// Run executes the seed with its own arguments and returns an exit code.
	Run(args []string, stdout, stderr io.Writer) int

	// Summary is a one-line description shown in help / listings.
	Summary() string
}

// registry maps a seed-type name (the first positional, e.g. "pi") to its
// built-in handler.
type registry map[string]Handler

// defaultRegistry returns the built-in seed types. Adding a new built-in seed
// is a one-line registration here (plus its handler). Unknown names are handled
// by the caller (the reserved PATH-plugin seam), NOT by this map.
func defaultRegistry() registry {
	return registry{
		"pi": piStub{},
	}
}

// names returns the registered seed names in sorted order, for stable help
// output and stable tests.
func (r registry) names() []string {
	out := make([]string, 0, len(r))
	for name := range r {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// printSeeds writes the registered seed types (name + summary) to w.
func printSeeds(w io.Writer, r registry) {
	for _, name := range r.names() {
		fmt.Fprintf(w, "  %-10s %s\n", name, r[name].Summary())
	}
}

// printHelp writes the top-level usage, including the dispatch surface.
func printHelp(w io.Writer, r registry) {
	fmt.Fprintf(w, "anonseed - seed a local-service-using tool's config into an anonymized identity.\n\n")
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  anonseed <seed> [args...]\n")
	fmt.Fprintf(w, "  anonseed --help\n")
	fmt.Fprintf(w, "  anonseed --version\n\n")
	fmt.Fprintf(w, "Seed types:\n")
	printSeeds(w, r)
	fmt.Fprintf(w, "\nRun 'anonseed <seed> --help' for a seed's options.\n")
}
