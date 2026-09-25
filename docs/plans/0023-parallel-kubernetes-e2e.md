# Parallel Kubernetes E2E execution

Status: implementation complete; cluster validation pending user run.
Date: 2026-09-25. Branch: `codex/parallel-kubernetes-e2e`.
Baseline: `4074d2b`, clean working tree.

## Objective and boundaries

Run the existing kind E2E scenarios with four concurrent namespace owners by
default, configurable from one to five. Preserve all existing assertions and the
single kind cluster. The user will run Kubernetes E2E validation; this task must
not start clusters, workloads or E2E scenarios.

## Decisions and entry gates

The operator watches only its Pod namespace and uses a namespaced leader Lease.
The chart also creates cluster-scoped RBAC named from the Helm release and installs
CRDs. Assign a distinct release to each scenario and install CRDs once before
workers start. Each worker uses its own namespace, provider, proxy, target, Secret,
ConfigMap, PVC and operator. The existing lifecycle and HTTP recovery sequences
have internal dependencies and stay ordered within their workers.

## Checkpoints

- [x] Split the original scenario stream without removing assertions.
- [x] Bound and validate parallelism; isolate releases and cluster setup.
- [x] Add cleanup, diagnostics, CI configuration and documentation.
- [ ] User runs sequential and four-worker E2E suites, then reports timings and
      failures for stabilization.

## Progress and evidence

The former single shell script contained one Helm release and one namespace.
The new parent owns kind/image/CRD lifecycle; five scenario scripts use shared
namespace-local fixtures. All worker exit statuses are collected before the parent
returns. Failed namespace diagnostics are retained under `dist/e2e-logs/`; the
optional keep-cluster flag preserves failed namespaces and the cluster.

Kubernetes E2E execution and timing are deliberately deferred to the user.
Offline checks passed: `sh -n` for every E2E shell file; the isolated invalid
parallelism Python test; `make helm-check`; `make docs`; workflow YAML parsing;
and distinct Helm render names for two releases. A source-line comparison found
all original nonblank scenario lines retained after splitting, aside from Helm
setup/teardown moved into the parent or made release-specific. Python PyYAML
was unavailable, so Ruby's built-in YAML parser validated the workflow instead.

## Resume and handoff

Run `EGRESSFOX_E2E_PARALLELISM=1 make e2e-kind` and
`EGRESSFOX_E2E_PARALLELISM=4 make e2e-kind` with repeated four-worker runs.
Inspect per-scenario logs and seconds. Pay particular attention to Helm RBAC
coexistence, lifecycle CRD retention while other workers run, and concurrent
managed runtime scheduling. Fix any observed race or resource limit before
claiming the parallel suite is stable or faster.
