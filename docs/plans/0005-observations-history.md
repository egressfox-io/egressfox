# M4: Bounded observations and persistent history

Status: in progress. Prepared: 2026-09-19. Started: 2026-09-19.
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
- [ ] Complete architecture/security review, documentation, full validation and
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

The probe checkpoint authorizes every locally resolved destination address before
execution, rejects redirects and environment proxies, pins the dial to one authorized
literal while preserving HTTP Host and TLS SNI, and bounds engine readiness, request
time and response bytes. Each request renders and natively validates a one-record
gateway, runs one isolated engine process, and produces either a revision-specific
observation or a safe execution failure. The scheduler enforces global, per-logical-
endpoint, per-target, queue and total-job limits with cancellation and stable result
positions. Its 1,000-job benchmark completed in 2.47 ms/op on the recorded development
host (performance evidence only, not a cross-platform target).

The official checksum-verified Mihomo v1.19.31 and sing-box v1.14.1 Darwin arm64
binaries both passed the opt-in controlled test: each client was natively validated,
started with a one-revision configuration, and returned a successful HTTP observation
through a local TLS Trojan server. Ordinary tests remain offline and skip this fixture
unless the pinned binary paths are supplied explicitly.

## Resume and handoff

Next: complete owning documentation and the M4 architecture/security review, then run
the full repository validation and close this execution record.
