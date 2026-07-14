# 2026-07-10 — `--allow` exemption guardrail: layering, naming, and reuse decisions

Decisions made while building `allow-exemption-guardrail` (spec `anonseed-config-seeder`, stories 8 + 19). Recorded here so the done record can link them; the load-bearing layering also lives in `docs/adr/0002` and in the `internal/allowguard` package doc.

## The pre-check-vs-authoritative layering (see ADR-0002)

anonseed's `internal/allowguard` is a FAIL-FAST pre-check, NOT the security boundary. anonctl's `internal/lanexempt` stays authoritative (re-validated on every `add`/`update`). anonseed keeps a small aligned COPY of the policy because `lanexempt` is Go-internal (un-importable) and there is no pure-validate anonctl CLI verb to shell out to (`anonctl verify` is a live egress prover, the wrong tool; spec story 26 forbids re-implementing a prover). Full rationale + the drift mitigations in `docs/adr/0002`; the extract-if-a-third-consumer follow-up is `work/notes/ideas/allow-guardrail-extract-to-anoncore.md`.

## Concept-coherence: two `Exception`s, split by layer (deliberate)

The task asked for "an `Exception` type parsed+validated from an `IP|CIDR:port` string", but `internal/seed` ALREADY has an `Exception` type: the DECLARATIVE plan carrier `{Allow string, Reason string}` (JSON-serialised into a `SeedPlan`, its fields frozen by `TestSeedPlanIsConfigSeedingOnly`). Those are two different things wearing anonseed's one ubiquitous word "Exception" (CONTEXT.md glossary term).

Decision: keep BOTH, split by LAYER, in different packages:

- `seed.Exception` — the declarative INTENT carrier (a raw string + reason), unchanged.
- `allowguard.Exception` — the VALIDATED value (parsed `*net.IPNet` + port), the guardrail's output.

A seed applier parses the raw `seed.Exception.Allow` through `allowguard.Parse` and lands the value on success. This mirrors anonctl's own split (its `"allow"` config STRING vs its validated `lanexempt.Exempt`), so it is the same concept at two layers, not a duplicated/re-meant concept. Alternative considered: rename the validated type (e.g. `allowguard.Exempt`, anonctl's word) to avoid two `Exception`s. Dropped: "Exemption/Exception" is anonseed's chosen word (task title, CONTEXT.md, ADR-0001), and the two live in different packages so there is no literal Go collision; the layer split is documented at both sites + ADR-0002. Touches: `internal/seed` (the carrier), any future seed applier (task anonctl-target-file-conventions) that will call `allowguard.Parse` on a `seed.Exception.Allow`.

## Reuse scope: what leans on anoncore `endpoint` vs what is anonseed-local

The task said to reuse anoncore's `endpoint` classification (`Classify`, loopback/Tor-port logic, `DefaultHost`) where it overlaps. Reality check (read on disk): anonctl's `lanexempt` is SELF-CONTAINED and does NOT import `endpoint`; the genuine PUBLIC overlap anoncore exposes is only `endpoint.DefaultHost` (`"127.0.0.1"`) and `endpoint.Classify` (which recognises the 9050/9150 Tor-SOCKS subset as `ClassTorShared`). The loopback-detection helper and the broader port set are unexported/anonctl-local.

Decision: reuse exactly the genuine overlap, no more.

- `endpoint.DefaultHost` — the loopback host constant (`allowguard.loopbackHost`), so anonseed and anonctl name the loopback host from one place.
- `endpoint.Classify` — used by `isAnonTorPort` to recognise the loopback Tor-SOCKS ports (9050/9150), rather than re-spelling those literals. A package test (`TestTorPortRecognitionReusesEndpointClassify`) pins the reuse so a future anoncore change to the Tor-port set is caught.
- anonseed-LOCAL policy (NOT in `endpoint`): the RFC1918 + link-local containment, the STRICTER loopback blocklist's broader ports ({53 clear-DNS, 9051 Tor CONTROL, 1080 generic SOCKS}), the `:53`-on-LAN refusal, and the mandatory-port rule. These are the allow-list POLICY, mirrored VERBATIM from `lanexempt`.

I did NOT force `endpoint.Parse` into service for the allow value: `endpoint.Parse` models a `socks5h://host:port` PROXY endpoint (it requires the socks5h scheme and refuses a bare `IP:port` without one), which is the wrong shape for a direct-egress allow value. Using it would be a mis-reuse; only `DefaultHost` + `Classify` genuinely fit.

## Test matrix: byte-aligned with anonctl's `lanexempt`

`allowguard_test.go`'s accept/reject vectors are byte-aligned (same inputs accepted/rejected) with anonctl's `internal/lanexempt` test matrix, read on disk from `/home/wighawag/dev/github/wighawag/anonctl`. Accept: RFC1918 + link-local (bare IP -> /32, whole-block CIDR:port), loopback non-anonymizer ports, non-53 LAN ports incl. DoT/853. Reject: public/broad (incl. the straddling `10.0.0.0/7`), hostnames (incl. `localhost`), port-omitted (both classes), `:53` on LAN, the loopback anonymizer ports ({53,9050,9150,9051,1080}), and malformed/bad-port values. This is the drift tripwire ADR-0002 relies on.
