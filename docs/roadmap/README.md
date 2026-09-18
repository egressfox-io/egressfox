# Implementation roadmap

This document owns milestone order and completion status. The
[feature catalog](features.md) owns priority assignments. P0 is the initial product,
not a single implementation task. A milestone can span several focused task
branches/plans; each must leave usable, tested work without pretending the whole
product is finished.

## Current state

M0 (repository foundation) is complete as of 2026-09-18. M1–M6 have not started.
There is no EgressFox executable, parser, probe, database adapter, renderer,
publisher, operator, CRD, or installation chart. Only repository tooling is
executable today. The bootstrap stops at M0.

**Next task: M1 — normalized endpoint identity and provenance-preserving deduplication.**
Use the [ready execution plan](../plans/0002-endpoint-identity.md). Every later stage
depends on distinguishing endpoints reliably; implementing a network parser or
controller first would harden an unreviewed identity model.

## P0 milestones

| Milestone | Dependency | Coherent outcome and exit criteria |
| --- | --- | --- |
| M0 — Repository foundation | Existing license/repository | Agent map, authoritative designs/ADRs, threat model, priority catalog, resumable plans, checked development commands, and reviewed commits. No product behavior. |
| M1 — Identity and inventory primitives | M0; resolve Q1 | Small Kubernetes-independent endpoint package with canonical identity contract/version, semantic validation for an explicit initial subset, provenance union, deterministic deduplication, safe diagnostics, and equivalence/rotation/permutation tests. No fetching/probing/rendering. |
| M2 — Safe source-to-inventory path | M1; resolve Q2 | One documented useful protocol/format slice through local input and bounded HTTP acquisition, normalization and static filters. Explicit unsupported input, transactional refresh/empty-source behavior, Secret-ready reference boundary, parser fuzz/adversarial tests, basic structured diagnostics and source metrics. Minimal CLI entry points only where backed by real use cases. |
| M3 — Validated engine artifacts and file publication | M2; resolve Q6 and file Q7 | Useful minimal routing/common model rendered for both Mihomo and sing-box with explicit capability errors, deterministic goldens, pinned native validation, and recoverable file/LKG publication. A simple deterministic all/explicit selection baseline enables a complete slice before adaptation. Controlled proxy traffic smoke test proves the output is usable. CLI validate/render/run foundations are documented and tested. |
| M4 — Bounded observations and persistent history | M3 supplies engine configuration; resolve Q3/Q4 | Through-endpoint, destination-aware checks using existing engines with explicit vantage and attribution; bounded scheduler, freshness/outcome semantics, SQLite schema/migrations/retention and restart tests. Baseline latency/availability summaries, probe metrics and explainable evidence. Load tests demonstrate budgets on large inventories. |
| M5 — Adaptive standalone reconciliation | M4; resolve Q5 | Compare static/lowest-latency baselines with a documented adaptive algorithm; integrate hard eligibility, Top-N, hysteresis/residence/recovery/cooldown and emergency failover into standalone run. Persist decision state, prove deterministic replay and restart behavior, measure failure scenarios and switch costs, expose bounded metrics. Both renderers and LKG file output participate in the full loop. |
| M6 — Kubernetes BYO operator and Secret delivery | M5; resolve Q8/Q9 and Secret Q7 | Review the alpha API and topology, generate with real pinned Kubebuilder, implement focused pool/gateway reconcilers using core use cases, scoped Secret references/watches, status/Conditions, safe owned Secret output, leader-scoped scheduling and durable restart behavior. Helm install/upgrade, envtest, kind/RBAC/traffic tests pass. No managed runtime or EgressPolicy required. |

Q identifiers refer to the [decision queue](../decisions/open-questions.md).
Before calling a milestone complete, update this roadmap, the support/compatibility
claims and examples it introduced, its execution plan, and relevant tests. A partial
slice must state its limits. Do not ship arbitrary placeholder enum values for future
protocols, strategies, or outputs.

M2–M3 must decide the initial protocol/format subset explicitly; “supports both
engines” does not mean “supports every engine protocol.” Native capability checks,
destination-aware observations, and compact decision reasons are P0 correctness
requirements even though rich capability discovery, multiple destination policies,
and user-facing explain tools come at P1.

P0 release readiness additionally requires documented threat controls actually
implemented for shipped paths, a supported version matrix, a private security
reporting process, dependency/license review, and honest operational limits.
No dates or quality claims are inferred from this milestone sequence.

## Later horizons

P1 develops managed runtimes, richer policy/composition/destination/diversity
features, operational tooling, additional outputs, and HA once its state design is
resolved. P2 adds advanced storage/UI/customization/rollout ideas. P3 explores deeper
datapath/fleet integration. The full [catalog](features.md) preserves these ideas;
they are not authorization to build them during a P0 task.

Dependencies may justify revising priorities through a design change. In particular,
P1 HA cannot assume the P2 PostgreSQL adapter already exists; Q9 must resolve that
relationship before implementation.
