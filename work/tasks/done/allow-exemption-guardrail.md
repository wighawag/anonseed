---
title: The --allow exemption type + fail-fast pre-validation (not the authoritative guardrail)
slug: allow-exemption-guardrail
spec: anonseed-config-seeder
blockedBy: [bootstrap-go-module-and-cli-skeleton]
covers: [8, 19]
---

## What to build

The `Exception` type (a validated `IP|CIDR:port` direct-egress hole) and a LIGHTWEIGHT pre-validation anonseed runs before writing an exemption into `defaults.json`. Crucially, this is a FAIL-FAST UX check, NOT the security boundary: the AUTHORITATIVE guardrail stays anonctl's (its `internal/lanexempt`, re-validated on every `add`/`update`), so a bad value anonseed somehow wrote is still rejected by anonctl at apply time — anonseed's pre-check just fails early with a clear message instead of letting a bad value sit until the next `add`.

The pre-validation mirrors anonctl/netcage's `--allow` rules (accept RFC1918 + link-local, and loopback `127.0.0.1:<port>` via a stricter check that rejects anonymizer control/SOCKS/DNS ports; reject public / hostname / `:53` / port-omitted — a port is MANDATORY). REUSE anoncore's `endpoint` classification primitives (`endpoint.Classify`, loopback/Tor-port logic, `DefaultHost`) where they overlap, so only the allow-list POLICY is anonseed-local, not the address parsing.

End to end: a good LAN/loopback `IP:port` parses to a valid `Exception`; a public IP / hostname / `:53` / missing-port value is rejected with a clear message — matching anonctl's accept/reject set.

## Acceptance criteria

- [ ] An `Exception` type parsed+validated from an `IP|CIDR:port` string; a port is mandatory (no all-ports form).
- [ ] Accept set: RFC1918 + link-local LAN; loopback `127.0.0.1:<port>` via a stricter check (reject `:53`, Tor SOCKS/control, generic SOCKS ports). Reject set: public, hostname/non-IP, `:53`, port-omitted.
- [ ] The accept/reject cases are BYTE-ALIGNED with anonctl's `internal/lanexempt` (share the same test vectors in spirit — the same inputs accepted/rejected).
- [ ] Reuses anoncore `endpoint` classification where it overlaps (do not re-derive address parsing anoncore already exposes).
- [ ] The layering is explicit in code + docs: anonseed's check is a FAIL-FAST PRE-CHECK; anonctl's apply-time re-validation is authoritative.
- [ ] Tests cover the accept/reject matrix (LAN ok, loopback-stricter, public/hostname/:53/no-port rejected).

## Blocked by

- `bootstrap-go-module-and-cli-skeleton` (needs the module + package layout).

## Prompt

> Goal: give anonseed an `Exception` type + a fail-fast pre-validation for `--allow` values, WITHOUT pretending to BE the security boundary. Domain: anonseed declares a direct-egress hole (`IP:port`) that a seeded tool needs (its LAN/loopback model), written into anonctl's `/etc/anonctl/defaults.json` `"allow"` list. anonctl RE-VALIDATES every default through its own guardrail when `add` runs, so that is the authoritative check; anonseed's job is only to fail EARLY on a bad value (better UX than letting it sit until the next `add` fails).
>
> Rules to mirror (from anonctl's README + `internal/lanexempt`): accept RFC1918 + link-local; accept loopback `127.0.0.1:<port>` via a STRICTER check that rejects `:53` and anonymizer control/SOCKS/DNS ports (Tor 9050/9150/9051, generic 1080); reject public, hostname/non-IP, `:53`, and port-omitted (a port is MANDATORY — the all-ports form is a deanonymization vector). REUSE anoncore's `endpoint` package classification (`Classify`, the loopback/Tor-port logic, `DefaultHost`) where it overlaps so only the allow-list POLICY is anonseed-local.
>
> IMPORTANT layering (write it into the code comments + an ADR): this pre-validation is CONVENIENCE (fail-fast), NOT the authoritative guardrail. anonctl's `internal/lanexempt` is authoritative and Go-INTERNAL (un-importable), and there is no anonctl pure-validate CLI verb to shell out to (`anonctl verify` is a LIVE egress prover needing root + a provisioned account — the WRONG tool, and spec story 26 forbids anonseed re-implementing an egress prover). So anonseed keeps a small aligned copy of the POLICY. WRITE an ADR in `docs/adr/` recording: the pre-check-vs-authoritative-apply-time layering, the drift risk, and a follow-up idea (`work/notes/ideas/`) to extract the guardrail into anoncore IF a third consumer appears (which would reopen anoncore ADR-0001, currently pinning lanexempt as anonctl-per-tool).
>
> FIRST, check against reality: read anonctl's `internal/lanexempt` accept/reject cases and anoncore's `endpoint` classification on disk; align to what actually landed.
>
> Test the accept/reject matrix. Done = the `Exception` type + pre-validation + the ADR + follow-up idea, gate green.
