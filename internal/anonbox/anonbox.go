// Package anonbox is the anonbox substrate applier: the DOWNSTREAM half of the
// anonbox target axis that consumes a seed.SeedPlan and delivers it onto the
// anonbox (netcage-backed, image-based) substrate.
//
// # Deliberate flagged non-delivery (this is a STUB)
//
// anonbox does not exist yet, so this applier is a STUB. Applying to anonbox is
// a LOUD, HONEST not-yet-available outcome: Apply always returns ErrNotYetAvailable
// (an error, so a caller cannot mistake it for success), and it performs NO I/O
// on the way there (no partial write, no crash). This keeps the target axis
// HONEST: anonbox is a real, DECLARED target the driver can route to and a seed
// can list in its Targets(), while the real image-based delivery is a visible,
// flagged non-delivery rather than a silent no-op-success or a panic. See spec
// story 23 (task anonbox-target-stub).
//
// Contrast the anonctl applier (task anonctl-target-file-conventions), which is
// buildable NOW because the anonctl substrate is home-on-host with shipped file
// conventions. This applier is the same DOWNSTREAM shape (consume a SeedPlan,
// see nothing of the seed) but for a substrate that is not here yet.
//
// # The intended real anonbox delivery (recorded so the later fill-in knows the shape)
//
// When anonbox exists, this applier will do the SAME home + exception seeding as
// the anonctl applier AND additionally stage the container image that has the
// tool installed. Concretely, the intended shape is:
//
//   - Home seeding: land the SeedPlan.Files into the anonbox account's home,
//     through the SAME safe-write surface the anonctl applier uses
//     (internal/homewrite -> anoncore seedhome: setuid/setgid/sticky stripped,
//     symlinks refused, mode-700, create-only). This half is identical to
//     anonctl's: a seed's files are substrate-agnostic.
//
//   - Exception declaration: declare each SeedPlan.Exception (the direct-egress
//     --allow holes) into anonbox's equivalent of anonctl's defaults `"allow"`
//     list, each value passing the fail-fast allowguard pre-check first, exactly
//     as the anonctl applier does.
//
//   - Image staging (the anonbox-ONLY addition): provide or stage the container
//     IMAGE that has the tool already installed. anonbox is image-based like
//     netcage (a machine = a dedicated host account + an assigned netcage
//     container + a persistent home), so the tool is baked into the image
//     substrate rather than installed on the host. This is the piece the anonctl
//     applier has no analogue for, and the piece that genuinely blocks on anonbox
//     existing.
//
// # webveil for anonbox comes FROM THE IMAGE (spec story 22b)
//
// On the anonbox target, webveil (SearXNG) is provided by the STAGED IMAGE:
// SearXNG is baked into the image and already running, so nothing is installed
// on the host and webveil points at the in-image socket. This is the proven
// anon-pi/netcage model. It DIFFERS from the anonctl target, where webveil is
// wired over a per-account host Unix socket that the seed detects and points at
// (task pi-seed-webveil-anonctl-socket, spec story 22c). So the anonbox webveil
// story is NOT implemented here; it is RECORDED so the later image-staging
// implementation wires webveil from the image rather than re-deriving it. On
// anonbox, webveil needs NO --allow exception either (an in-image socket has no
// IP/port), same as the anonctl socket path.
//
// # Why an error, not a bool or a silent skip
//
// A "skip" already has a precise meaning on the target axis: a seed that does not
// declare a target in its Targets() is SKIPPED by the driver (seed.Run), a normal
// non-fatal outcome that is NOT mis-seeding. That is DIFFERENT from what happens
// here: a seed that DOES declare anonbox, routed to anonbox, cannot yet be
// delivered. Reusing "skip" for that would conflate "this seed does not apply to
// this substrate" with "this substrate is not implemented yet". So the
// not-yet-available outcome is a distinct, loud ERROR value, sitting DOWNSTREAM
// of the driver's skip decision.
package anonbox

import (
	"context"
	"errors"

	"github.com/wighawag/anonseed/internal/seed"
)

// Target is the substrate this applier delivers onto. It is re-exported from the
// seed package so a caller wiring the target axis (the --target flag +
// detection, task target-flag-and-detection) can name the anonbox target through
// this applier's own surface. It is exactly seed.TargetAnonbox.
const Target = seed.TargetAnonbox

// ErrNotYetAvailable is the LOUD not-yet-available outcome of applying a seed
// plan to the anonbox substrate. anonbox does not exist yet, so Apply always
// returns this error (wrapped) rather than reporting a silent success. A caller
// can test for it with errors.Is, so the not-yet-available case is a first-class,
// matchable outcome and never looks like "it worked".
var ErrNotYetAvailable = errors.New("anonbox target not yet available")

// Apply is the anonbox substrate applier entry point: the DOWNSTREAM seam that
// (when anonbox exists) would land the SeedPlan's files into the anonbox account
// home, declare its exceptions, AND stage the tool's container image (see the
// package doc for the intended shape). While anonbox does not exist, it is a
// deliberate flagged non-delivery: it does NO I/O and returns an error wrapping
// ErrNotYetAvailable, so the outcome is loud and honest (never a silent
// no-op-success, never a crash).
//
// The signature mirrors the intended real applier (context + the plan to apply)
// so wiring the target axis against this stub does not change when the real body
// lands: only Apply's body is filled in. The plan is accepted and ignored
// deliberately, so this stub sits at the same call site the real applier will.
func Apply(_ context.Context, _ seed.SeedPlan) error {
	// Loud, honest, and side-effect-free: return the not-yet-available error
	// WITHOUT touching any substrate. When anonbox exists, this body is replaced
	// by the home+exception seeding + image staging described in the package doc.
	return ErrNotYetAvailable
}
