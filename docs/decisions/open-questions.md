# Decision queue

This is the authoritative queue of unresolved architectural choices. An open item
is not permission to improvise a default in public API or persistent data. Resolve
it in the linked design and task plan before its gate; use an ADR only if the result
is durable enough to warrant one. No individual maintainers are assigned yet;
the agent/contributor undertaking the gated milestone owns bringing evidence.

| ID | Question and decision evidence needed | Gate | Design owner |
| --- | --- | --- | --- |
| Q5 | Adaptive score definition, eligibility/freshness/confidence, hysteresis/residence/recovery/cooldown rules, emergency failure criteria and tuning. Compare against baselines on reproducible traces; no arbitrary production constants. | M5 | [Selection](../designs/observations-and-selection.md) |
| Q7 | Kubernetes Secret generation/backup ownership and later runtime activation/reload/rollback acknowledgment. File publication is resolved by ADR 0008. | Secret at M6; reload P1 | [Publication](../designs/policy-rendering-publication.md) |
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
| Q2 | [ADR 0007](0007-safe-source-snapshots.md) defines bounded inline/HTTP acquisition, URI-list/Base64 formats, strict transactional snapshots, explicit partial/empty policy, stable source identity and disappearance semantics. |
| Q6 | [ADR 0008](0008-engine-artifacts-and-file-publication.md) defines the minimal common gateway, pinned Mihomo/sing-box profiles, deterministic renderers and mandatory exact-byte native validation. Native composition remains Q10. |
| Q7 (file) | [ADR 0008](0008-engine-artifacts-and-file-publication.md) defines protected journal/receipt recovery, no-op/LKG/ownership behavior and one-writer file publication. Secret publication remains M6; reload/activation rollback remains P1. |
| Q3 | [ADR 0009](0009-bounded-probes-and-sqlite-evidence.md) defines isolated one-revision engine probes, target authorization, vantage and outcome attribution, execution-failure separation and scheduler budgets. |
| Q4 | [ADR 0009](0009-bounded-probes-and-sqlite-evidence.md) defines the CGo-free SQLite driver, versioned schema, WAL/FULL single-writer durability, protected state, retention and deterministic summaries. |
