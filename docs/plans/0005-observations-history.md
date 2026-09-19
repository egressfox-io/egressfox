# M4: Bounded observations and persistent history

Status: complete. Prepared: 2026-09-19. Started: 2026-09-19. Completed: 2026-09-19.
Branch/baseline: `feat/observations-history` from `424f5aa`.

## Objective and boundaries

Implement revision-, target- and vantage-specific observations, isolated
through-engine HTTP probes, bounded scheduling, SQLite schema/history/retention and
deterministic baseline summaries. The owning design is
[observations and selection](../designs/observations-and-selection.md), with Q3/Q4
resolved by [ADR 0009](../decisions/0009-bounded-probes-and-sqlite-evidence.md).

No adaptive score, ranking, Top-N, selection feedback, daemon, Kubernetes API,
distributed probe fleet, production metrics exporter, reload or M5 behavior belongs
here. The pre-existing untracked `Archive.zip` observed during discovery was not task
input and must remain unstaged if it is present.

## Decisions and entry gates

M4 uses one pinned isolated engine process per observation and the M3 one-endpoint
configuration boundary. Initial probes are bounded HTTP GET requests with explicit
target authorization. Execution failures do not become health observations. The
history database is a protected local SQLite file using the CGo-free ncruces driver,
schema version 1, WAL/FULL durability, one writer, transactional three-dimensional
retention and complete evidence keys.

## Checkpoints

- [x] Record Q3/Q4, target/outcome/vantage/revision semantics and dependency choice.
- [x] Implement immutable observation/target types, confidential revision persistence
  boundary, freshness and deterministic summaries.
- [x] Implement schema migration, insertion/query/restart and bounded SQLite retention.
- [x] Implement target authorization, isolated Mihomo/sing-box execution and bounded
  HTTP probing without protocol reimplementation.
- [x] Implement bounded scheduler and representative load/benchmark evidence.
- [x] Prove controlled through-engine observation for both pinned engines and restart
  replay; keep ordinary tests offline.
- [x] Complete architecture/security review, documentation, full validation and
  coherent commits; leave a reviewable branch without task artifacts.

## Progress and evidence

Discovery reviewed Git state/history, AGENTS.md, roadmap/catalog, architecture,
observation/selection and renderer/publication designs, threat model, testing/workflow,
all ADRs and open questions, M1 identity, M2 source semantics, and M3 renderers,
artifact validation, publication and controlled sing-box fixture. Official SQLite,
Go driver and `x/net/proxy` documentation was reviewed on 2026-09-19.

The first implementation checkpoint adds an explicit protected endpoint-revision
persistence boundary plus safe target ID/revision, vantage, complete evidence keys,
immutable observations and deterministic window/freshness summaries. Tests prove
credential rotation, target and vantage isolation, canonical targets, boundary-time
behavior, permutation replay and formatting/JSON redaction.

The SQLite checkpoint pins the CGo-free driver and implements protected-file opening,
WAL/FULL durability with one database connection, schema-v1 migration and verification,
idempotent insertion, exact-key replay and summaries, and transactional age/per-key/
global pruning. Tests cover restart replay, evidence-key isolation, schema rejection,
permissions and symlinks, cancellation, concurrent writers, and raw-secret absence from
the database and its WAL/SHM sidecars.

The probe checkpoint authorizes every locally resolved endpoint and target address
before execution, requires independent intent for their private ranges, rejects
redirects and environment proxies, and pins both dials to authorized literals while
preserving HTTP Host and TLS SNI. It bounds engine readiness, request time and response
bytes. Each request renders and natively validates a one-record
gateway, runs one isolated engine process, and produces either a revision-specific
observation or a safe execution failure. The scheduler enforces global, per-logical-
endpoint, per-target, queue and total-job limits with cancellation and stable result
positions. Its 1,000-job benchmark completed in 2.14 ms/op on the recorded development
host (performance evidence only, not a cross-platform target).

The official checksum-verified Mihomo v1.19.31 and sing-box v1.14.1 Darwin arm64
binaries both passed the opt-in controlled test: each client was natively validated,
started with a one-revision configuration, and returned a successful HTTP observation
through a local TLS Trojan server. Ordinary tests remain offline and skip this fixture
unless the pinned binary paths are supplied explicitly.

The final architecture/security review added independent endpoint-address
authorization before rendering. Endpoint and target hostnames are both resolved
locally, every answer is checked, and execution-only literal addresses prevent the
engine from performing a second policy-bypassing resolution. Original endpoint
identity/revision and TLS names remain the evidence semantics. Future M5 selection
can consume summaries without importing engines, SQL or Kubernetes; source refresh,
credential rotation, target rotation, vantage changes and engine profile changes all
retain separate evidence.

Final validation on Darwin arm64 / Apple M4 Pro:

- `make fmt` — passed; no formatting changes remained.
- `make check` — passed, including `go vet`, all race-enabled tests, build,
  documentation validation and whitespace checks. It ran outside the restricted
  network sandbox because existing local HTTP fixtures require loopback bind.
- `make docs` — passed; offline links and anchors are valid.
- `make vuln` — passed with pinned `govulncheck` v1.8.0; no vulnerabilities found.
- `go test -race -count=5 ./internal/probe` — passed.
- `go test -race -count=5 ./internal/state` — passed at the SQLite checkpoint.
- `EGRESSFOX_MIHOMO_BINARY=... EGRESSFOX_SINGBOX_BINARY=... go test -race
  -count=1 -run TestPinnedEnginesProduceControlledObservations -v ./internal/probe`
  — both checksum-verified pinned engines passed.
- `go test -run '^$' -bench BenchmarkSchedulerThousandJobs -benchtime=5x
  ./internal/probe` — 2,140,867 ns/op, 2,911,342 B/op, 47,148 allocs/op for 1,000 jobs.
- `go test -run '^$' -bench BenchmarkStoreAppendAndLoadWindow -benchtime=100x
  ./internal/state` — 225,077 ns/op, 74,683 B/op, 1,120 allocs/op.
- `git diff --check` — passed. Official engine archives matched the SHA-256 values
  recorded in ADR 0008; binaries and temporary runtime state remained outside the
  repository.

## Resume and handoff

M4 is complete on `feat/observations-history`. M5 is next and remains gated by Q5:
use this revision-specific persistent evidence to research and define eligibility,
adaptive scoring, hysteresis/residence/recovery and emergency replacement. No M5
selection or reconciliation behavior was implemented here.
