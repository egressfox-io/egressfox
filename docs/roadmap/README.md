# Implementation roadmap

This document owns milestone order and completion status. The
[feature catalog](features.md) owns priority assignments. P0 is the initial product,
not a single implementation task. A milestone can span several focused task
branches/plans; each must leave usable, tested work without pretending the whole
product is finished.

## Current state

M0–M5 are complete as of 2026-09-19. M6 (Kubernetes BYO operator and Secret
delivery) and the post-M6 release-hardening milestone are complete as of 2026-09-21.
The repository now contains the complete P0 control-plane path and a deliberate
release process for the first development version with Q12 resolved, exact source/checksum provenance,
SBOMs, vulnerability gates, keyless signing/provenance design and a non-publishing
dry run. P1 M7 adds the first explicit managed engine path and exact-generation
activation. There is still no product CLI, managed HTTP source refresh,
cross-namespace API or HA topology.

**Next implementation milestone: P1 M8.** The authoritative
[P1 product roadmap](p1.md) now orders managed runtime/activation, resilient source
refresh, target-aware selection, bounded routing policy, observability and
explainability. M7 is complete; later P1 behavior is not implemented. A public
alpha is a separate maintainer decision and requires the protected repository
settings in the release guide.

## P0 milestones

| Milestone | Dependency | Coherent outcome and exit criteria |
| --- | --- | --- |
| M0 — Repository foundation | Existing license/repository | Repository guidance, authoritative designs/ADRs, threat model, priority catalog, resumable plans, checked development commands, and reviewed commits. No product behavior. |
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

## P1 and later horizons

The [P1 product roadmap](p1.md) is the single authority for its objective, six
milestones, dependencies, scope tiers and exit criteria. The full
[catalog](features.md) preserves optional and later ideas without turning them into
implicit milestones.

## Accepted post-P1 architecture track

After M12, the roadmap naturally continues into the accepted
[runtime/standalone design](../designs/runtime-and-standalone.md) and
[artifact/publication/composition design](../designs/artifact-publication-and-composition.md).
This is future work, not an implementation commitment with dates. It reuses the
shared primitives established by M8–M12 instead of duplicating refresh, profiles,
policy, visibility or explanation in another frontend.

Candidate work stages, to be decomposed and gated before implementation:

| Stage | Future outcome |
| --- | --- |
| S1 | Thin runtime execution/lifecycle boundary and canonical engine packaging |
| S2 | One-shot Go `egressfox` CLI and shared publisher adapters |
| S3 | Long-running standalone: publication-only output and optional managed activation |
| S4 | Full single-file distribution, secure lazy payload materialization and release qualification |

The future publisher family includes Kubernetes Secret, Vault, S3-compatible object
storage, filesystem and sensitive one-shot stdout export. A separate Kubernetes
publication/API milestone will deliberately design `EgressOutput` and migration
from current Gateway `outputSecretName`; basic managed Gateway does not require
`EgressOutput`. Exact stage boundaries and sequencing, output API/schema, runtime
mechanics and release artifact procedures remain open until their owning designs
and plans supply implementation evidence. These stage labels do not reuse P1
milestone numbers.

The standalone `run` control plane must work publication-only as well as in managed
standalone mode. Publication-only continuously synchronizes external destinations
without owning a permanent Gateway; compatible real engine executables are still
required when native validation or through-endpoint probes run, and may be invoked
as temporary helpers. Both modes reuse the shared Go core. Exact CLI flags and
configuration remain future design work.

P2 holds advanced storage, operator HA, cross-namespace grants, native composition,
additional output systems beyond the accepted initial publisher family, and richer
UI/customization. P3 explores transparent datapath and fleet integration. Operator
HA cannot assume leader election or a shared SQLite file is sufficient; it must
supersede the single-active topology in ADR 0011 with supported durable state and
fencing.
