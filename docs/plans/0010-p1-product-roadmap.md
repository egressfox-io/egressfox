# P1 product roadmap

Status: complete.
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
- [x] Publish one authoritative P1 objective, ordered milestones, dependency graph,
  scope tiers, API direction, entry gates, exit criteria and first milestone.
- [x] Synchronize product, architecture/design summaries, feature catalog, decision
  queue and documentation map without duplicating the roadmap.
- [x] Run documentation/repository validation, review the complete diff, commit
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

The completed roadmap defines one product objective and six ordered milestones:
M7 managed gateway/activation, M8 resilient HTTP sources, M9 target-aware profiles,
M10 bounded EgressPolicy routing, M11 operational metrics and M12 explainability/P1
acceptance. M7 is first because it closes the usable-Service gap with the most P0
leverage. BYO remains compatible; exact CRD fields are gated rather than frozen.

Q13–Q17 now gate managed-runtime ownership, source caching, named profiles, routing
semantics and explanation exposure. Q7 remains the activation gate, Q10 owns
unscheduled native composition/diversity, and Q11 contributes the narrow CLI
contract. No ADR was accepted for speculative fields: M7 must record its exact
runtime/API decision before generating CRDs or controllers.

Optional P1 ideas are separated from the must-have path. Engine-specific live
reload, runtime replicas/PDB/topology, source-only diversity, one demand-backed
protocol and quota metadata require a deliberate roadmap revision. Operator HA,
PostgreSQL, cross-namespace grants, Vault/generic outputs, native composition,
transparent routing, broad protocols, Web/TUI and predictive selection are deferred
beyond P1.

Research used exact Mihomo 1.19.31 and sing-box 1.14.1 source/documentation plus
current official Kubernetes Deployment, Pod readiness, immutable Secret and
NetworkPolicy behavior and Prometheus label guidance. Links and the inference they
support are recorded in the authoritative roadmap. Version-sensitive behavior must
be rechecked during implementation.

Validation results:

- `GOCACHE=/tmp/egressfox-go-cache make docs` — passed.
- `GOCACHE=/tmp/egressfox-go-cache make check` — first sandboxed attempt reached
  race tests but the sandbox denied an `httptest` IPv6 loopback bind; the required
  rerun with local loopback permission passed generated drift, vet, all race tests,
  build, docs, Helm lint/render, release-manifest validation and whitespace checks.
- `git diff --check` — passed.

No Go, CRD, generated manifest, Helm template, engine profile, persistence schema or
release artifact changed. No release, tag, merge or push was performed.

## Resume and handoff

The next task is P1 M7 only: resolve Q7/Q13 in an ADR and implement the managed
single-replica authenticated SOCKS Gateway for both exact engine profiles, including
owned immutable generation Secrets, Deployment/ClusterIP Service, restart-based
activation, readiness/status separation, failed-rollout LKG preservation, security
defaults, API migration and envtest/kind traffic coverage. Do not begin M8 source
caching, target profiles, EgressPolicy, metrics/CLI expansion or optional P1 work.
