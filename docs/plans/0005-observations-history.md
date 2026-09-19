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

- [ ] Record Q3/Q4, target/outcome/vantage/revision semantics and dependency choice.
- [ ] Implement immutable observation/target types, confidential revision persistence
  boundary, freshness and deterministic summaries.
- [ ] Implement schema migration, insertion/query/restart and bounded SQLite retention.
- [ ] Implement target authorization, isolated Mihomo/sing-box execution and bounded
  HTTP probing without protocol reimplementation.
- [ ] Implement bounded scheduler and representative load/benchmark evidence.
- [ ] Prove controlled through-engine observation for both pinned engines and restart
  replay; keep ordinary tests offline.
- [ ] Complete architecture/security review, documentation, full validation and
  coherent commits; leave a reviewable branch without task artifacts.

## Progress and evidence

Discovery reviewed Git state/history, AGENTS.md, roadmap/catalog, architecture,
observation/selection and renderer/publication designs, threat model, testing/workflow,
all ADRs and open questions, M1 identity, M2 source semantics, and M3 renderers,
artifact validation, publication and controlled sing-box fixture. Official SQLite,
Go driver and `x/net/proxy` documentation was reviewed on 2026-09-19.

## Resume and handoff

Next: update the owning design and decision queue, validate the decision checkpoint,
then implement the observation domain before its SQLite and engine consumers.
