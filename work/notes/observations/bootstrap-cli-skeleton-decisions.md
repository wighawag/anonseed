# 2026-07-10 — anonseed CLI skeleton: in-scope build decisions

Decisions made while building `bootstrap-go-module-and-cli-skeleton` (the tracer bullet). Recorded here so the done record can link them; each also lives as a comment at its code site.

## Dispatch / registry shape

The dispatch lives in `internal/cli` (not `package main`), so the CLI/handler seam is testable without spawning a binary. `main.go` is a two-line entrypoint that calls `cli.Run(os.Args[1:], os.Stdout, os.Stderr) -> exitcode` and never calls `os.Exit` itself from within `Run` (tests drive `Run` directly and assert on exit code + captured stdout/stderr).

The registry is a `map[string]Handler` keyed by seed name (`registry.go`, `defaultRegistry()`), where `Handler` is a deliberately tiny interface (`Run(args, stdout, stderr) int` + `Summary() string`). Adding a built-in seed is a one-line registration. The richer future seed contract (resolve target home, anoncore `seedhome`, `--allow`, the api-key guard) is meant to grow AROUND this seam, not replace it. Alternative considered: a big `switch` in `main` — rejected because a registry keeps the unknown-seed seam (below) in one place and keeps `main` thin.

## The unknown-seed seam (reserved PATH-plugin hook)

An unknown first positional (`anonseed nope`) fails loudly (exit 2) and lists the available seeds. Per CONTEXT.md "PATH-plugin (reserved)" and spec story 24, this error site is exactly where the future git/kubectl-style fallback (`anonseed foo` -> exec PATH `anonseed-foo`) will hook in. That fallback is NOT built here (speculative until a third-party seed exists); only the seam is stood up. No new concept was introduced — this reuses the already-glossed "PATH-plugin (reserved)" term.

## How `--version` is sourced

`--version` reports a package-level `var version = "dev"` in `internal/cli` (NOT a const, NOT `runtime/debug.BuildInfo`). It is overridable at build time via the linker: `go build -ldflags "-X github.com/wighawag/anonseed/internal/cli.version=v0.1.0"`. Chosen so a release can stamp a clean tag without a source edit while a plain `go build` still yields a usable dev string. A test (`TestVersionOverridable`) pins that the reported string tracks the var.

## LICENSE vs the spec "Out of Scope" note (drift resolved toward the task)

The spec launch snapshot lists "A `LICENSE` file — not written by this scaffold" under Out of Scope, but the TASK's acceptance criteria (authored later, at tasking time) explicitly require a verbatim AGPL-3.0 `LICENSE` and module metadata declaring AGPL-3.0-only. The task is the operative spec, so I wrote `LICENSE` (verbatim FSF AGPL-3.0 text, sha256 `0d96a4ff68ad6d4b6f1f30f713b18d5184912ba8dd389f86aa7710db079abcb0`) and added `// SPDX-License-Identifier: AGPL-3.0-only` to `main.go` (Go has no license field in `go.mod`, so an SPDX header is the idiomatic declaration). Recorded because a reader of the spec alone might expect no LICENSE.

## go.mod language version

`go mod init` defaulted to `go 1.25.0` under the go1.26 toolchain; I set it to `go 1.26` via `go mod edit -go=1.26` to match the family convention the task names ("e.g. go 1.26").
