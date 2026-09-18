# ADR 0004: SQLite for initial standalone history

Date: 2026-09-18. Status: accepted for standalone mode.

## Context

Historical availability, latency, streaks, and residence state should survive
restart. Requiring an external database would make an initial standalone binary
much harder to operate. History must not become unbounded in memory or CR status.

## Decision

Use SQLite for the initial standalone persistent state, behind the narrow
history/decision operations actual consumers need. Use explicit schema migrations,
bounded retention, transactional updates, and recovery tests. Driver, schema,
journal/durability tuning, and backup policy remain open until M4.

Do not build a generic persistence framework. PostgreSQL remains a future P2
backend. This decision does not choose the operator's HA storage or authorize
multi-replica access to a shared SQLite file.

## Alternatives

Memory-only state loses anti-flapping/history on restart. JSON files complicate
incremental transactional updates and queries. PostgreSQL initially would impose
an external service on the standalone path. CR status is neither private credential
storage nor a time-series database.

## Consequences

SQLite write concurrency, disk pressure, migrations, and backups need deliberate
handling. Its [WAL constraints](https://sqlite.org/wal.html) preclude treating it
as a network-filesystem HA database. Keep raw secrets out of historical measurements;
state protection and recovery belong in [observations and selection](../designs/observations-and-selection.md).
