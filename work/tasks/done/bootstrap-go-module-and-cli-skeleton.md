---
title: Bootstrap the Go module and anonseed CLI skeleton (green gate)
slug: bootstrap-go-module-and-cli-skeleton
spec: anonseed-config-seeder
blockedBy: []
covers: [1, 3, 4, 5]
---

## What to build

The greenfield tracer bullet that turns the currently-red `verify` gate green. Initialise the Go module (`github.com/wighawag/anonseed`), a minimal `anonseed` CLI whose argv dispatches to per-seed-type subcommands (so `anonseed pi` routes to a stub that prints a not-yet-implemented notice and exits cleanly), an AGPL-3.0-only `LICENSE`, and a `--version`/`--help` surface. End to end: `go build ./...` produces an `anonseed` binary, `anonseed pi` runs the (stub) dispatch, and `gofmt`/`go vet`/`go test` all pass.

This establishes the family conventions: Linux + Go, run-together naming (`anonseed`, no hyphen, part of anonctl/anonbox/anonseed/anoncore), and the subcommand-per-seed-type shape (`anonseed <seed> ...`) that later seeds plug into.

## Acceptance criteria

- [ ] `go mod init github.com/wighawag/anonseed` done; `go.mod` targets a current Go (matching the family, e.g. go 1.26).
- [ ] `anonseed` builds to a binary; `anonseed --help` lists the dispatch surface; `anonseed --version` prints a version.
- [ ] `anonseed pi` dispatches to a stub subcommand (clean exit, clear not-yet-implemented message) — proving the per-seed-type routing.
- [ ] An unknown subcommand (`anonseed nope`) fails loudly with a helpful message (this is the seam the future PATH-plugin fallback will hook into, but the fallback itself is NOT built here).
- [ ] `LICENSE` is the verbatim AGPL-3.0 text; any module metadata declares AGPL-3.0-only.
- [ ] The `.dorfl.json` `verify` gate (`gofmt -l . && go vet ./... && go build ./... && go test ./...`) runs GREEN.
- [ ] Tests cover the dispatch (known seed routes to its handler; unknown fails) — mirror standard Go table-test style.

## Blocked by

- None — can start immediately.

## Prompt

> Goal: stand up the anonseed Go CLI skeleton so the repo builds green and later seed tasks have a subcommand surface to plug into. anonseed is a Go CLI (Linux) that seeds a local-service-using tool's config into an anonymized identity; the FIRST seed type is `pi`, invoked `anonseed pi ...`. Naming is run-together, no hyphen (family: anonctl / anonbox / anonseed / anoncore). Default license is AGPL-3.0-only (verbatim FSF text as `LICENSE`).
>
> FIRST, check this task against current reality (launch snapshot; may have drifted): the repo is greenfield (no `go.mod`, no sources yet) and the `verify` gate is currently RED only because of that. If sources already exist, reconcile rather than clobber.
>
> Build: `go mod init github.com/wighawag/anonseed`; a `main` that parses argv and dispatches the first positional to a seed-type handler (a registry keyed by seed name, so `pi` -> a stub handler; unknown -> a loud, helpful error that is the future PATH-plugin hook point, NOT built now); `--help` and `--version`. Keep it minimal — this is the tracer bullet, not the seed logic. Test the dispatch at the CLI/handler seam (known seed routes, unknown errors). Done = the binary builds, `anonseed pi` runs the stub, and `gofmt`/`go vet`/`go build`/`go test` are all green.
>
> RECORD any non-obvious in-scope decision (e.g. the dispatch/registry shape, how `--version` is sourced) durably and link it from the done record.
