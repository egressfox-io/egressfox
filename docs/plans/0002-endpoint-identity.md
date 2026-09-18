# M1: Endpoint identity and provenance-preserving deduplication

Status: complete. Prepared: 2026-09-18. Started: 2026-09-18. Completed: 2026-09-19.
Branch/baseline: `feat/endpoint-model` from `0f77c1e`.

## Objective and scope

Implement the smallest useful Kubernetes-independent endpoint model, semantic
identity and provenance-preserving deduplication, with no source I/O or product CLI.
This is the one recommended next task in the [roadmap](../roadmap/README.md).
The owning design is [endpoints and sources](../designs/endpoints-and-sources.md).

No subscription parser, probe engine, storage adapter, score, renderer, publisher,
controller, Rust workspace, or full protocol union belongs in this task.

## Entry gates

Read [ADR 0001](../decisions/0001-control-plane-and-core.md),
[ADR 0002](../decisions/0002-go-and-module.md), and
[Q1](../decisions/open-questions.md). Inspect existing work and create the task branch.

Resolve Q1 before committing an identity contract: compare synthetic examples of
supported connection semantics, decide identity scope/version, credential rotation
and fingerprint privacy, and specify which fields are identity-bearing versus
metadata. Explain hash/privacy tradeoffs and any future migration impact in the
owning design. Do not expose a secret-derived fingerprint as a public identifier.

Select a minimal protocol subset sufficient to exercise host/port, credentials,
transport and TLS distinctions without inventing every protocol structure. Reject
unsupported/unknown connection semantics explicitly. The exact Go shape follows
this exercise; it is not dictated by the plan.

## Checkpoints and acceptance

- [x] Write/review the canonicalization and credential-revision contract in the design.
- [x] Add `internal/endpoint` with cohesive real types/functions and no Kubernetes,
  engine, database, network, or speculative public SDK dependencies.
- [x] Implement deterministic identity and semantic validation for the declared subset.
- [x] Implement deterministic deduplication preserving source associations and aliases
  without treating duplicate subscriptions as independent endpoint failures.
- [x] Prove equivalence and distinction, source/display rename invariance, input-order
  invariance, credential/TLS/transport change behavior, unknown-field rejection,
  and safe diagnostics using synthetic fixtures and table/property tests.
- [x] Update documentation and support limits; run the repository checks and inspect
  the full diff. Commit coherent checkpoints and leave a clean task branch.

## Validation plan

Use `make fmt`, `make check`, and `make vuln`, plus focused tests in the new package.
Tests must demonstrate behavior, not merely mirror struct fields. Include a test
that two differently named records with identical supported connection semantics
deduplicate while two differing credentials do not share incompatible probe identity.
Do not introduce live-network tests. Record actual results here when executed.

## Decisions and evidence

Q1 is resolved by [ADR 0006](../decisions/0006-versioned-endpoint-identity.md).
M1 uses a safe logical endpoint ID plus a confidential connection revision. A
credential rotation keeps logical continuity and changes the full connection
identity, preventing incompatible observation reuse. The initial semantic slice is
VLESS and Trojan with TCP/WebSocket and ordinary TLS; unsupported options remain
explicitly outside the model. Official sing-box and Mihomo outbound/transport/TLS
documentation was reviewed on 2026-09-18 before fixing the slice.

The first implementation checkpoint adds canonical addresses, VLESS/Trojan
credentials, TCP/WebSocket transport, ordinary TLS, versioned logical IDs, and
private connection revisions. Focused race tests, vet, formatting, and whitespace
checks pass. The stable version 1 ID has a golden test.

The inventory checkpoint models provenance as source-to-connection relationships
with source-local records and alias sets. Deduplication sorts by full identity,
unions relationships and aliases, preserves multiple credential revisions, checks
complete configuration equality after key matches, and returns sanitized conflicts.
All input permutations produce the same result and re-deduplication is idempotent.
Adversarial formatter tests cover valid and invalid format verbs so secrets and
untrusted aliases remain behind explicit accessors.

The architecture review confirmed that source adapters can construct discoveries
without Kubernetes, provenance remains independently removable by source, probes
can carry the opaque full identity without credential access, and renderers can use
typed configuration without the domain importing either engine. Alias/source
changes preserve identity; credential changes preserve logical continuity while
invalidating the connection revision.

## Validation results

All results are from `feat/endpoint-model` on 2026-09-19 using Go 1.27.1:

- `make fmt` — passed.
- `go test -race -count=1 ./internal/endpoint` — passed.
- `go test ./internal/endpoint -run '^$' -fuzz '^FuzzAddressCanonicalRoundTrip$' -fuzztime=5s`
  — passed after approximately 2.5 million executions. The first run found the
  root-dotted IPv4 ambiguity fixed in `c6abbb5`.
- `go test ./internal/endpoint -run 'Diagnostics|CompleteURIs|Conflict' -count=100`
  — passed.
- `make check` — passed: formatting, vet, race tests, build, documentation, and
  whitespace checks.
- `make vuln` — passed with `No vulnerabilities found.` The first sandboxed call
  could not resolve `proxy.golang.org`; the network-authorized rerun completed.
- `git diff --check` — passed throughout the checkpoints.

Checkpoint commits before the completion documentation are `1f77673` (identity
decision), `077bca8` (canonical connection identity), `ec3591a` (provenance and
deduplication), and `c6abbb5` (fuzz-discovered IPv4 normalization fix).

## Handoff

M1 is complete on `feat/endpoint-model`. The package supports the documented VLESS
and Trojan subset, version 1 logical IDs, confidential credential-sensitive
revisions, provenance relationships, and deterministic deduplication. Q1 is closed.

Q2 deliberately remains for M2: choose the first input formats/adapters and source
refresh admission semantics. M4 must define protected persistence for confidential
connection revisions before storing them. No source acquisition, probe, history,
selection, rendering, publication, or Kubernetes implementation was added here.
