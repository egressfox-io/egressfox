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
