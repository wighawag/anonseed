package cli

import (
	"fmt"
	"io"
)

// piStub is the stub handler for the `pi` seed type (the first built-in seed).
//
// It proves the per-seed-type routing only: it prints a clear not-yet-implemented
// notice and exits cleanly (code 0). The real pi seed (probe the local endpoint's
// /v1/models, read the matching provider in ~/.pi/agent/models.json, synthesise
// models.json + settings.json into the target home, declare the --allow
// exception, enforce the api-key guard, wire webveil) is a later task.
type piStub struct{}

func (piStub) Summary() string {
	return "seed the pi tool's config (not yet implemented)"
}

func (piStub) Run(_ []string, stdout, _ io.Writer) int {
	fmt.Fprintln(stdout, "anonseed pi: the pi seed is not yet implemented.")
	fmt.Fprintln(stdout, "This is the tracer-bullet stub proving per-seed-type dispatch works.")
	return 0
}
