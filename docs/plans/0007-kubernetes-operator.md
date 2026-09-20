# M6: Kubernetes BYO operator and Secret publication

Status: active. Prepared: 2026-09-20. Started: 2026-09-20.
Branch/baseline: `feat/kubernetes-operator` from `1c26ae3`.

## Objective and boundaries

Expose M1–M5 through a namespace-scoped Kubernetes operator: generated alpha APIs,
Secret-backed sources, focused pool/gateway controllers, native validation, owned
Secret publication, durable SQLite state, install manifests and Helm delivery.
[ADR 0011](../decisions/0011-namespaced-byo-operator.md) resolves Q7 (Secret), Q8
and Q9. Managed engines, EgressPolicy, activation/reload, HA and cross-namespace
references remain outside M6.

## Decisions and entry gates

Use the checksum-verified Kubebuilder v4.16.0 Go v4 namespace-scoped scaffold,
controller-runtime v0.25.1, Kubernetes libraries v0.37.0, controller-tools v0.22.0
and Kubernetes 1.37 test assets. The operator is one replica with leader election
and a RWO PVC. Source/target bytes and output artifacts live only in Secrets or the
protected local state boundary; status and diagnostics remain safe.

## Checkpoints

- [x] Discover existing contracts, review upstream compatibility, resolve Q7–Q9.
- [ ] Integrate generated API/tooling and structural CRDs.
- [ ] Implement Secret source admission, owned Secret publication and tests.
- [ ] Implement indexed pool/gateway reconcilers and operator composition.
- [ ] Add namespace-scoped RBAC, secure deployment, Helm chart and samples.
- [ ] Add envtest and kind lifecycle/RBAC tests.
- [ ] Complete documentation, generation drift checks and all validation.

## Validation evidence

Pending. Record exact commands and unavailable environmental checks before marking
the plan complete.

## Risks and deferred work

Native probe processes are bounded but expensive; reconciliation must avoid
unbounded worker occupancy. Kubernetes API publication proves stored desired bytes,
not BYO engine activation. Q10–Q12, managed runtimes, reload acknowledgment,
cross-namespace grants and HA remain deferred.
