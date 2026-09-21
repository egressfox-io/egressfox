# P1 product roadmap

Status: in progress.
Date: 2026-09-22. Branch: `codex/p1-product-roadmap`. Baseline: `2c6ae58`.

## Objective and boundaries

Turn the broad post-P0 feature inventory into one authoritative, dependency-ordered
P1 roadmap at `docs/roadmap/p1.md`. Define the product outcome, milestone boundaries,
API direction, entry gates, acceptance criteria and deliberate deferrals needed for
future agents to implement one milestone at a time.

This is a design-only task. It does not add or change CRDs, controllers, renderers,
runtime workloads, source adapters, protocols, outputs, persistence schemas or
release artifacts. Stable P0 behavior remains frozen; discrepancies found during
review are recorded rather than repaired implicitly.

## Decisions and entry gates

ADRs 0001 through 0012 remain accepted. In particular, the Kubernetes-independent
core, exact engine profiles, validated publication/LKG behavior, deterministic
selection, namespaced BYO topology and release contract constrain P1.

The design must settle milestone ordering without prematurely freezing speculative
fields. Runtime activation/reload remains Q7; policy/native composition and
diversity semantics remain Q10; explain access/redaction remains Q11. New P1 gates
identified by this task belong in the decision queue and the owning designs. A
durable runtime/API choice should be recorded by the implementation milestone after
its exact contract and compatibility evidence are complete.

## Checkpoints

- [x] Inspect Git/history, repository guidance, product/roadmap/catalog, architecture,
  all designs and ADRs, decision queue, CRDs, controllers, selection, Helm and
  operations documentation.
- [x] Review exact-profile upstream runtime/reload behavior and relevant Kubernetes
  rollout, Secret, readiness and networking contracts.
- [ ] Publish one authoritative P1 objective, ordered milestones, dependency graph,
  scope tiers, API direction, entry gates, exit criteria and first milestone.
- [ ] Synchronize product, architecture/design summaries, feature catalog, decision
  queue and documentation map without duplicating the roadmap.
- [ ] Run documentation/repository validation, review the complete diff, commit
  coherent changes and leave the branch clean.

## Progress and evidence

P0 review confirms a complete deterministic path from Secret-backed source input
through revision-specific probing, SQLite evidence, adaptive selection, exact-profile
rendering/native validation and owned Secret publication. The operator is
namespace-scoped, single-active with one RWO SQLite PVC. It creates no engine
workload or proxy Service, and `Published=True` says nothing about runtime loading.

The largest adoption gap is therefore between a valid Secret and a usable egress
endpoint. Mihomo 1.19.31 exposes an authenticated REST configuration reload, while
sing-box 1.14.1 handles `SIGHUP` by checking configuration, closing the current
instance and constructing another. That asymmetric behavior is not a common atomic
activation contract. Kubernetes Secret projection is eventually consistent, while
an immutable generation Secret referenced by a changed Pod template gives a clearer
rollout boundary. Kubernetes Deployment completion and readiness can prove the
expected process generation is serving its listener; they cannot prove arbitrary
destination traffic is healthy.

The current common model exposes only a loopback SOCKS listener, one selected set
and a final route. `ProxyPool` combines source inventory, one probe target and one
selection policy; `EgressGateway` references one pool and publishes one mutable
Secret. P1 must preserve existing objects while separating inventory, target-aware
selection, routing policy and runtime lifecycle deliberately.

## Resume and handoff

Draft `docs/roadmap/p1.md` first. Keep its milestone detail authoritative and make
other documents link to it. Recommend a managed single-replica SOCKS Gateway with
restart-based exact-generation activation as the first slice, subject to explicit
API, credential, Secret-retention, rollout and status gates. Do not add P1 code or
generated Kubernetes artifacts.
