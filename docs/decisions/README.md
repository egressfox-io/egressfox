# Architecture decisions

ADRs record durable accepted decisions and why alternatives were rejected. Designs
own evolving details. [Open questions](open-questions.md) are not accepted ADRs.
Implementation status belongs in the [roadmap](../roadmap/README.md).

| ADR | Decision | Status |
| --- | --- | --- |
| [0001](0001-control-plane-and-core.md) | Control plane with Kubernetes-independent core | Accepted |
| [0002](0002-go-and-module.md) | Go and one deliberately small module | Accepted |
| [0003](0003-operator-tooling.md) | Kubebuilder/controller-runtime, alpha API, deferred generation | Accepted |
| [0004](0004-sqlite-history.md) | SQLite for initial standalone historical state | Accepted |
| [0005](0005-validated-publication.md) | Validate exact artifacts and preserve last-known-good | Accepted |
| [0006](0006-versioned-endpoint-identity.md) | Versioned logical endpoint IDs and confidential connection revisions | Accepted |
| [0007](0007-safe-source-snapshots.md) | Bounded source snapshots with transactional replacement | Accepted |
| [0008](0008-engine-artifacts-and-file-publication.md) | Pinned engine artifacts and journaled file publication | Accepted |
| [0009](0009-bounded-probes-and-sqlite-evidence.md) | Isolated engine probes and bounded SQLite evidence | Accepted |
| [0010](0010-deterministic-adaptive-selection.md) | Deterministic adaptive selection and publication checkpoints | Accepted |
| [0011](0011-namespaced-byo-operator.md) | Namespace-scoped BYO operator and owned Secret publication | Accepted |
| [0012](0012-release-distribution-and-provenance.md) | Engine redistribution, versioning, signing and provenance | Accepted |
| [0013](0013-managed-gateway-activation.md) | Managed gateway ownership and exact-generation activation | Accepted |
| [0014](0014-development-versioning-and-kubernetes-compatibility.md) | Development release identity and Kubernetes compatibility | Accepted |
| [0015](0015-standalone-runtime-and-engine-packaging.md) | Standalone control plane, runtime boundary and engine packaging | Accepted future architecture |
| [0016](0016-artifact-publication-and-availability.md) | Change-driven artifacts, independent publication and safe activation | Accepted future architecture |
| [0017](0017-leaf-pools-and-policy-composition.md) | Leaf ProxyPools and composable policy candidates | Accepted future architecture |
| [0018](0018-resilient-http-sources.md) | Managed HTTP source cache, freshness and refresh contract | Accepted |
| [0019](0019-subscription-endpoint-semantics.md) | Bounded JSON, VMess/Shadowsocks and identity v2 semantics | Accepted |
| [0020](0020-subscription-identity-and-source-destination-policy.md) | Durable subscription identity and source destination permissions | Accepted |
| [0021](0021-version-three-connection-semantics-and-capabilities.md) | M8.5 v3 connection identity and exact-profile capability gate | Accepted |
| [0022](0022-c3-reality-and-transport-profile.md) | Bounded C3 Reality, transports and sing-box uTLS build revision | Accepted; managed qualification pending |

## Adding a decision

Use the next four-digit number and a descriptive filename. Include title, date,
status, context, decision, alternatives, and consequences. Link relevant designs
and only sources that influenced the choice. No approval committee or separate
template is required. Review the ADR with the change; do not turn speculation into
an accepted record to unblock code.

For reversal, add a superseding ADR, mark the earlier one superseded, and update
this index and current designs. Preserve the reasoning history. Routine dependency
patch upgrades do not require an ADR; changing a security boundary, public API
strategy, persistence model, or control-plane responsibility usually does.
