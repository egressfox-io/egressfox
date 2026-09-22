# Decision queue

This is the authoritative queue of unresolved architectural choices. An open item
is not permission to improvise a default in public API or persistent data. Resolve
it in the linked design and task plan before its gate; use an ADR only if the result
is durable enough to warrant one. No individual maintainers are assigned yet;
the contributor undertaking the gated milestone owns bringing evidence.

| ID | Question and decision evidence needed | Gate | Design owner |
| --- | --- | --- | --- |
| Q5 | Resolved by [ADR 0010](0010-deterministic-adaptive-selection.md): common hard eligibility, Wilson-adjusted latency, deterministic Top-N, residence/hysteresis and failure cooldown/recovery with receipt-bound restart state. Defaults remain evaluation inputs rather than universal optimality claims. | Resolved in M5 | [Selection](../designs/observations-and-selection.md) |
| Q10 | Native escape-hatch merge/ownership rules, fragments/base composition, multiple policy precedence, unknown diversity domains and multi-source attribution. Specify unsupported/infeasible behavior. | P1 before those features | [Renderers](../designs/policy-rendering-publication.md) and [selection](../designs/observations-and-selection.md) |
| Q11 | Stable CLI schema/exit behavior, explain authorization/redaction, rich Rust/Ratatui TUI value, Web UI interfaces. Start with actual operational use cases; no second workspace now. | CLI at M3/M5; UI later | [Feature catalog](../roadmap/features.md) |
| Q14 | HTTP source union, conditional-validator/cache identity, protected cache schema, fallback expiry, retry/backoff budget and status semantics. | M8 before persistent cache/API | [Sources](../designs/endpoints-and-sources.md) and [P1 roadmap](../roadmap/p1.md) |
| Q15 | Named target-profile identity, mapping from legacy `probe`/`selection`, bounded status, inventory-by-profile scheduler budget and profile lifecycle cleanup. | M9 before CRD/code | [Selection](../designs/observations-and-selection.md), [Kubernetes](../designs/kubernetes.md) and [P1 roadmap](../roadmap/p1.md) |
| Q16 | Exact common routing matches/actions for the pinned engines, ordered-rule/DNS semantics, profile references, explicit final behavior, infeasible groups and legacy implicit-policy migration. | M10 before EgressPolicy CRD | [Renderers](../designs/policy-rendering-publication.md), [Kubernetes](../designs/kubernetes.md) and [P1 roadmap](../roadmap/p1.md) |
| Q17 | Bounded latest-decision report, opt-in disclosure, deterministic truncation, authorization/RBAC, CLI machine-output versioning and status-size budget. | M12 before explanation API/CLI | [Selection](../designs/observations-and-selection.md), [Kubernetes](../designs/kubernetes.md) and [P1 roadmap](../roadmap/p1.md) |

Operator HA and PostgreSQL are now both deferred beyond the committed P1 path. A
future HA design must choose supported durable state and fencing; it must not be
implemented by casually increasing replicas or sharing SQLite.

## Resolved gates

| ID | Resolution |
| --- | --- |
| Q1 | [ADR 0006](0006-versioned-endpoint-identity.md) defines identity version 1, credential rotation, private revision handling, and the initial semantic slice. Detailed canonicalization is in the [endpoint design](../designs/endpoints-and-sources.md). |
| Q2 | [ADR 0007](0007-safe-source-snapshots.md) defines bounded inline/HTTP acquisition, URI-list/Base64 formats, strict transactional snapshots, explicit partial/empty policy, stable source identity and disappearance semantics. |
| Q6 | [ADR 0008](0008-engine-artifacts-and-file-publication.md) defines the minimal common gateway, pinned Mihomo/sing-box profiles, deterministic renderers and mandatory exact-byte native validation. Native composition remains Q10. |
| Q7 (file) | [ADR 0008](0008-engine-artifacts-and-file-publication.md) defines protected journal/receipt recovery, no-op/LKG/ownership behavior and one-writer file publication. Secret publication was subsequently resolved by ADR 0011; reload/activation rollback remains P1. |
| Q3 | [ADR 0009](0009-bounded-probes-and-sqlite-evidence.md) defines isolated one-revision engine probes, target authorization, vantage and outcome attribution, execution-failure separation and scheduler budgets. |
| Q4 | [ADR 0009](0009-bounded-probes-and-sqlite-evidence.md) defines the CGo-free SQLite driver, versioned schema, WAL/FULL single-writer durability, protected state, retention and deterministic summaries. |
| Q7 (Secret) | [ADR 0011](0011-namespaced-byo-operator.md) defines validated, owner-checked Secret publication, protected receipts, no-op/LKG behavior and owner-reference deletion. Runtime activation remains open. |
| Q8 | [ADR 0011](0011-namespaced-byo-operator.md) defines the namespaced alpha resources, same-namespace Secret references, status boundaries, ownership and scoped RBAC. |
| Q9 | [ADR 0011](0011-namespaced-byo-operator.md) defines one active namespace-scoped process, leader election, one RWO PVC, restart reconstruction and explicitly unsupported HA. |
| Q12 | [ADR 0012](0012-release-distribution-and-provenance.md) permits source-built engine derivatives with GPL notices and complete corresponding source, brands the sing-box derivative separately under its additional name condition, preserves Apache-2.0 for EgressFox, and defines protected keyless release signing, SBOM, provenance and private reporting boundaries. External repository settings remain required before publication. |
| Q7 (managed) | [ADR 0013](0013-managed-gateway-activation.md) defines restart-based exact-generation activation, authenticated readiness, old-ready LKG retention and separate Published/Activated/RuntimeReady states. Engine-specific live reload remains optional later work. |
| Q13 | [ADR 0013](0013-managed-gateway-activation.md) defines the explicit managed union, generated client auth, fixed listener, release-image authority, exact-owned resource set, opaque immutable generations, rollout, cleanup, transitions and deletion behavior. |
