// Package seed defines the built-in-seed interface: the hard-to-reverse core
// abstraction every seed type implements, and the driver that runs a seed
// against a chosen target.
//
// The shape is deliberately narrow and STRICTLY DECLARATIVE. A seed declares
// two things and only two things:
//
//   - Files to write into a target identity's home (home-relative path + bytes).
//   - Exceptions: the direct-egress `--allow` holes the seeded tool needs
//     (possibly ZERO, e.g. for a Unix-socket-wired service; possibly more
//     than one).
//
// There is NO affordance to provision an account or to launch/start a runtime
// or service. That is a load-bearing invariant (see ADR docs/adr/0001): anonseed
// SEEDS config; account provisioning is anonctl/anonbox's job and runtime
// launch is anoncore/anonctl's job. The type surface here cannot express a
// "run this" instruction, so a later seed cannot quietly drift into launching.
//
// Seed-type and target are ORTHOGONAL axes. A seed declares which substrates it
// applies to via Targets(); the driver skips a seed for any target it does not
// declare (a non-fatal skip, never a mis-seed). Plan is PURE: it takes the
// chosen target as an input and synthesises a SeedPlan with no filesystem or
// network I/O. The interactive model-pick lives UPSTREAM of Plan (in opts) and
// the substrate writing lives DOWNSTREAM (the appliers, separate tasks); neither
// lives inside Plan.
package seed

import "context"

// Target names a substrate that delivers a seed's plan. Seed-type and target are
// orthogonal: one seed type (e.g. "pi") may apply to several substrates.
type Target string

const (
	// TargetAnonctl is the anonctl substrate: home-on-host, writing into
	// /etc/anonctl/default-home/ and /etc/anonctl/defaults.json. Buildable now.
	TargetAnonctl Target = "anonctl"

	// TargetAnonbox is the future image-based substrate (netcage-backed).
	// Reserved here so seeds can declare it; its applier is deferred.
	TargetAnonbox Target = "anonbox"
)

// FileToWrite is one config file a seed wants written into the target identity's
// home. Path is home-relative (e.g. ".pi/agent/models.json"); Content is the
// exact bytes to write. It carries no "run"/"exec" affordance by design.
type FileToWrite struct {
	// Path is relative to the target identity's home directory. It must not be
	// absolute and must not escape the home (the applier enforces that); this
	// type only declares intent.
	Path string `json:"path"`

	// Content is the exact file content to write.
	Content string `json:"content"`
}

// Exception is one direct-egress `--allow` hole a seeded tool needs: the
// endpoint it must reach directly while all other egress stays forced through
// the proxy (e.g. its LAN/loopback model server). A socket-wired service needs
// none, so a SeedPlan may carry zero Exceptions.
type Exception struct {
	// Allow is the `IP:port` endpoint to allow directly (the value that lands in
	// anonctl's `"allow"` list). Validation against anonctl's --allow guardrail
	// is the applier's job; this type only declares intent.
	Allow string `json:"allow"`

	// Reason is a short human-readable note for why this hole is needed, so a
	// seeded default is auditable. Optional.
	Reason string `json:"reason,omitempty"`
}

// SeedPlan is the pure, declarative output of a seed for a given target: the
// files to write plus the direct-egress exceptions to declare. Nothing else.
//
// It is JSON-serializable (round-trips losslessly) so the reserved PATH-plugin
// escape hatch can emit it on stdout as its contract. By construction it can
// express ONLY files + exceptions: there is no field that carries a command,
// service, or lifecycle instruction.
type SeedPlan struct {
	Files      []FileToWrite `json:"files"`
	Exceptions []Exception   `json:"exceptions"`
}

// Options carries the already-resolved, non-interactive inputs a seed needs to
// synthesise its plan. The interactive parts (which models to import, the
// default model, whether webveil is enabled) are resolved UPSTREAM of Plan and
// arrive here as plain data, so Plan stays pure and deterministic.
//
// It is intentionally minimal at this seam; individual seeds read the fields
// they need. Concrete seeds may embed richer, seed-specific option structs
// around this in their own packages.
type Options struct {
	// Endpoint is the local model endpoint's `host:port`, when the seed needs
	// one. Empty for seeds that do not.
	Endpoint string
}

// Seed is the internal contract every built-in seed type implements. It is
// strictly declarative: given resolved options and a chosen target, Plan
// synthesises the files-to-write and exceptions-to-declare. It performs no I/O
// and no interactivity.
type Seed interface {
	// Name is the seed-type name (e.g. "pi"), matching its CLI subcommand.
	Name() string

	// Targets lists the substrates this seed applies to. A seed need not support
	// every target; the driver skips it for any target absent from this list.
	Targets() []Target

	// Plan synthesises the SeedPlan for the given target. It is PURE:
	// deterministic, with no filesystem or network access. The target is an
	// input because a seed's plan may legitimately differ per substrate.
	Plan(ctx context.Context, opts Options, target Target) (SeedPlan, error)
}
