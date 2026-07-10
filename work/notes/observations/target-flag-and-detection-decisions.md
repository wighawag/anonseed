# 2026-07-10 — the --target axis: a reusable target package, the CLI pi-handler wiring, and the deferred interactive pick

Decisions made while building `target-flag-and-detection` (prd `anonseed-config-seeder`, stories 22f + 22g). Recorded here so the done record can link them; the load-bearing ones also live as doc comments at their choice sites (`internal/target`, `internal/cli/seed_pi.go`, `internal/cli/pi_production.go`).

## The `--target` axis lives in a NEW package `internal/target`, not inlined in the CLI

The flag parsing, the detection seam, the interactive detect-then-ask (`Select`), and the per-target fan-out (`Run`) are all in `internal/target`, a package ORTHOGONAL to seed-type (matching CONTEXT.md's "target substrate ... ORTHOGONAL to seed type"). The CLI's pi handler is only the WIRING that reads the flag and reports outcomes.

Rationale: the axis is reusable across every future seed type (`opencode`, ...), so it should not be inlined in the pi handler. Alternative considered: put it all in `internal/cli`. Dropped: that would fork the axis per seed handler. Touches: any future seed handler reuses `target.Parse` / `target.Select` / `target.Run` rather than re-implementing selection. No new NAMED domain concept is introduced (Target/detect/present/fan-out already exist in CONTEXT.md + the seed package); the package only groups them.

## Detection is behind a `Detector` seam; the production detector sniffs anonctl's base-dir existence

`target.Detector` is the environment-sniffing seam (tests inject a fake present/absent set). The production `EnvDetector` reports **anonctl present iff `/etc/anonctl` (its config root) exists** and **NEVER reports anonbox present** (anonbox does not exist; its applier is the loud not-yet-available stub, so routing an operator there would only produce an error).

Rationale for the anonctl signal: anonctl OWNS `/etc/anonctl` (the host state anonseed writes into), so its config root existing is the cheapest filesystem-only presence signal, no exec. This is a USER-VISIBLE choice (it decides what the default path offers), so it is recorded. Alternative considered: probe for the `anonctl` binary on PATH. Dropped for now: anonseed writes the FILE CONVENTIONS directly (it does not import or exec anonctl, per the anonctl applier's package doc), so the directory it writes into is the more honest signal than a binary that might be installed without its `/etc` state. Touches: `EnvDetector.AnonctlBaseDir` overrides the path (tests/non-standard installs); when anonbox ships, its presence signal is added in `detect.go`.

## `--target anonbox` on the pi seed SKIPS today (realizing position 2 of the anonbox-deferred note)

The sibling note `pi-seed-targets-anonbox-deferred.md` left an open question: when the target axis lands, does `--target anonbox` on the pi seed route to the not-yet-available stub, or SKIP (since the pi `Seed.Targets()` declares only anonctl)? It named two coherent positions and deferred the call to "the task that makes anonbox real" / the target-flag work.

Realized here: it SKIPS. The pi `Seed` declares `Targets() = [anonctl]` (unchanged by this task), so the driver (`seed.Run`) skips anonbox for the pi seed BEFORE the anonbox applier is reached; the operator sees a clean "target \"anonbox\" skipped (the pi seed does not support this substrate)" line, exit 0. This is position 2 of the note (the cleaner UX: a skip, not a loud not-yet-available, for a substrate that literally cannot run). It is a REALIZATION of the axis mechanics (the seed's declared `Targets()` bounds the fan-out), NOT a new decision to change the pi seed's `Targets()`. A future anonbox-fill-in task still owns whether the pi seed's `Targets()` GAINS anonbox. Touches: the pi seed's `Targets()` (unchanged); the future anonbox applier fill-in.

## The interactive model-PICK in the CLI is a non-interactive "import all" default, DEFERRED

The pi handler wires the pi seed's interactive `Resolve` (probe `/v1/models`, read the user's models.json, run the api-key guard) with real seams, BUT the model-PICK seam (`piseed.PickFunc`) is wired to a simple `importAllPick` (import every discovered candidate, first ID-sorted as default) rather than a rich TUI.

Rationale + scope: this task owns the `--target` AXIS. A rich interactive model-pick TUI is a substantial separate surface belonging to the pi seed's own CLI work (the `piseed` library already ships `Resolve`/`Plan`; `pi-seed-webveil-anonctl-socket` is the next pi CLI task). Building a TUI here would entangle the target axis with an unbuilt pi-CLI concern. The `importAllPick` default keeps `anonseed pi --target anonctl` FUNCTIONAL end to end (it produces a real plan) without inventing that TUI. This is a USER-VISIBLE default (what gets imported when no pick UI is shown), so it is recorded. Alternative considered: build the TUI now. Dropped as out-of-axis-scope. Touches: a future pi-CLI task that adds the real interactive pick replaces `importAllPick` (the seam is already there); the api-key guard + endpoint-scoping are UNAFFECTED (they run inside `piseed.Resolve` regardless of the pick).

## Two "not this substrate" outcomes kept distinct (coherence with the anonbox stub)

The fan-out (`target.Run`) reports three outcomes per target: Applied, Skipped (the driver's non-fatal "seed does not declare this target"), and Err (the seed's Plan failed OR the applier could not deliver, e.g. `anonbox.ErrNotYetAvailable`). Skip and not-yet-available are DELIBERATELY not conflated, mirroring the anonbox stub's own package-doc rationale ("Why an error, not a bool or a silent skip"): a skip is decided UPSTREAM by the driver, the applier error DOWNSTREAM. The handler surfaces a skip on stdout (informational, exit 0) and an error on stderr (loud, exit 1).
