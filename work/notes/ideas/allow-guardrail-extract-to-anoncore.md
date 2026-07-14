---
title: Extract the --allow guardrail into anoncore IF a third consumer appears
slug: allow-guardrail-extract-to-anoncore
type: idea
status: parked
---

# Extract the `--allow` exemption guardrail into anoncore (trigger: a third consumer)

## The situation today (2026-07-10)

The direct-egress `--allow` guardrail (parse+validate an `IP|CIDR:port` into a validated private-LAN/loopback exemption) currently lives in TWO places:

- **anonctl's `internal/lanexempt`** — the AUTHORITATIVE guardrail, re-validated on every `add`/`update`. Go-INTERNAL (un-importable) by design.
- **anonseed's `internal/allowguard`** — a fail-fast PRE-CHECK, an aligned COPY of anonctl's policy, so anonseed fails early on a bad `--allow` value instead of letting anonctl reject it later. NOT the security boundary (see `docs/adr/0002`).

anoncore ADR-0001 currently PINS `lanexempt` as anonctl-per-tool (deliberately NOT extracted into anoncore). anonseed's copy respects that: it does not import anonctl internals; it mirrors the policy and reuses only anoncore's already-public `endpoint` primitives (`endpoint.DefaultHost`, `endpoint.Classify`).

## The trigger and the idea

TWO consumers (anonctl authoritative + anonseed's fail-fast copy) is not enough to justify extraction: the policy is anonctl's, anonctl might still evolve it, and coupling anoncore to it for one non-authoritative copy adds shared-surface cost for little gain. The aligned copy + byte-aligned tests are the cheaper answer.

A THIRD consumer of the same guardrail would tip the balance. At that point, extract the guardrail (the parse+validate + the accept/reject policy) into anoncore as a shared, public package (alongside `endpoint`), and have anonctl, anonseed, and the third consumer all import it. That REOPENS anoncore ADR-0001 (which pins `lanexempt` as anonctl-per-tool), so it is a deliberate decision to make then, not a drift to let happen silently.

## What to check when the trigger fires

- Is the third consumer authoritative or another fail-fast copy? (If another copy, the same "not worth extracting" logic might still hold, now for three.)
- Does the extracted package belong next to `endpoint` in anoncore, or is it its own package? (It reuses `endpoint`, so co-location is plausible.)
- anonctl's `lanexempt` also emits diagnostics its nft layer consumes (`IsV4`, `IsLoopback`, `HostPort`); the extracted anoncore package must carry the pure parse+validate + those pure accessors, WITHOUT anonctl's nft-generation coupling.
- Update anoncore ADR-0001 and anonseed `docs/adr/0002` to record the reversal.

## Provenance

Spun out of the `allow-exemption-guardrail` task (spec `anonseed-config-seeder`, stories 8 + 19). The pre-check-vs-authoritative layering and the drift risk are recorded in `docs/adr/0002-allow-exemption-pre-check-not-the-authoritative-guardrail.md`; this note holds the extract-if-third-consumer follow-up so the trigger is not lost.
