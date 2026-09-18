# Decision queue

This is the authoritative queue of unresolved architectural choices. An open item
is not permission to improvise a default in public API or persistent data. Resolve
it in the linked design and task plan before its gate; use an ADR only if the result
is durable enough to warrant one. No individual maintainers are assigned yet;
the agent/contributor undertaking the gated milestone owns bringing evidence.

| ID | Question and decision evidence needed | Gate | Design owner |
| --- | --- | --- | --- |
| Q2 | First supported protocols/formats/source adapters; empty-source, partial-parse, stale inventory and disappearance policy. Specify strict admission and fixtures. | M2; protocol effects revisited at M3 | [Sources](../designs/endpoints-and-sources.md) |
| Q3 | Through-endpoint probe engine topology, vantage, endpoint attribution, remote DNS/network restrictions, target incident detection, budget defaults. Benchmark isolated prototypes and failure/load cases without adding proxy protocols. | M4 before probe execution | [Observations](../designs/observations-and-selection.md) |
| Q4 | SQLite driver/CGO, schema/migrations, retention, observation ordering, transaction/durability mode, clock discontinuity, restart/backup policy. Measure expected observation throughput and test recovery. | M4 before persistence contract | [History](../designs/observations-and-selection.md) |
| Q5 | Adaptive score definition, eligibility/freshness/confidence, hysteresis/residence/recovery/cooldown rules, emergency failure criteria and tuning. Compare against baselines on reproducible traces; no arbitrary production constants. | M5 | [Selection](../designs/observations-and-selection.md) |
| Q6 | Minimum common routing semantics, DNS behavior, protocol/transport support, target version/build matrix, validators and native-process isolation. Golden/native validation evidence per admitted feature. | M3; native composition P1 | [Renderers](../designs/policy-rendering-publication.md) |
| Q7 | File journal/receipt crash recovery, empty-pool and first-run behavior, ownership/retention versus revocation/expiry, emergency disable, Secret generation/backup model, activation/rollback acknowledgment. Specify failure matrix and interrupted-write tests. | File at M3, Secret at M6, reload P1 | [Publication](../designs/policy-rendering-publication.md) |
| Q8 | CRD names/scope/fields/defaults, pool-to-gateway references, inline P0 routing evolution to EgressPolicy, ownership/deletion, status truth tables, Secret references and RBAC. Review concrete examples and upgrade scenarios before generation. | M6 API gate | [Kubernetes](../designs/kubernetes.md) |
| Q9 | Operator durable state location, sharing coherent pool snapshots, single-active topology/fencing, volume/restart semantics; later HA without assuming shared SQLite or prematurely requiring PostgreSQL. | M6 single-active; P1 HA | [Kubernetes](../designs/kubernetes.md) |
| Q10 | Native escape-hatch merge/ownership rules, fragments/base composition, multiple policy precedence, unknown diversity domains and multi-source attribution. Specify unsupported/infeasible behavior. | P1 before those features | [Renderers](../designs/policy-rendering-publication.md) and [selection](../designs/observations-and-selection.md) |
| Q11 | Stable CLI schema/exit behavior, explain authorization/redaction, rich Rust/Ratatui TUI value, Web UI interfaces. Start with actual operational use cases; no second workspace now. | CLI at M3/M5; UI later | [Feature catalog](../roadmap/features.md) |
| Q12 | Private vulnerability channel, maintainer/release ownership, engine binary redistribution/license obligations, signing/SBOM and image provenance process. Existing Apache-2.0 license remains unchanged. | Before public runtime release or bundled engine distribution | [Security](../../SECURITY.md) |

The known P1-HA/P2-PostgreSQL tension is deliberate: HA must either use a supported
single-writer durable-state arrangement, choose another design, or explicitly
revise priorities. It must not be implemented by casually increasing replicas.

## Resolved gates

| ID | Resolution |
| --- | --- |
| Q1 | [ADR 0006](0006-versioned-endpoint-identity.md) defines identity version 1, credential rotation, private revision handling, and the initial semantic slice. Detailed canonicalization is in the [endpoint design](../designs/endpoints-and-sources.md). |
