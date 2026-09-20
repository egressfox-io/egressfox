# M6: Kubernetes BYO operator and Secret publication

Status: complete. Prepared: 2026-09-20. Started: 2026-09-20. Completed: 2026-09-21.
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
- [x] Integrate generated API/tooling and structural CRDs.
- [x] Implement Secret source admission, owned Secret publication and tests.
- [x] Implement indexed pool/gateway reconcilers and operator composition.
- [x] Add namespace-scoped RBAC, secure deployment, Helm chart and samples.
- [x] Add envtest and kind lifecycle/RBAC tests.
- [x] Complete documentation, generation drift checks and all validation.

## Progress and validation evidence

The generated alpha API enforces structural defaults, duration CEL bounds and
same-namespace references. The adapters compose M1–M5 without Kubernetes imports in
the core. Secret publication tests cover create, exact-byte no-op, replacement,
metadata preservation, ownership/type collisions, stale-input rejection, LKG
preservation, size limits and credential canaries. Fake-client and envtest coverage
protect indexes, watched relationships, Conditions, defaulting, validation and
owner references.

The real-cluster test found three integration constraints that unit/native checks
could not expose: a PVC root may not satisfy the protected-store mode, Podman kind
needs an OCI archive rather than Docker image loading, and sing-box v1.14 requires
an explicit resolver for outbound server domain names. The operator now creates a
private state subdirectory, the test supports both Docker and Podman loading, and
the sing-box renderer declares a local default resolver. The test uses authorized
ClusterIP literals for probe traffic, matching M4's SSRF-safe dial semantics.

Final validation on Darwin arm64 / Apple M4 Pro:

- `make check` — passed after generation drift checks, `go vet`, all race-enabled
  package tests, build, offline documentation validation, Helm lint/template and
  whitespace checks.
- `make test-envtest` — passed against checksum-pinned Kubernetes 1.37.0 API-server
  and etcd assets.
- `KIND_EXPERIMENTAL_PROVIDER=podman CONTAINER_CLI=podman make e2e-kind` — passed
  with kind v0.33.0 and the digest-pinned Kubernetes 1.37.0 node image. It built and
  loaded the operator image, checked negative RBAC, CR reconciliation, a real VLESS
  probe and BYO sing-box traffic, Secret repair, restart no-op, rejected credential
  rotation preserving LKG, Helm upgrade/uninstall and retained CRDs.
- `make vuln` — passed with pinned `govulncheck` v1.8.0; no called
  vulnerabilities remain. The first final scan found GO-2026-6348 in transitive
  gRPC v1.82.1; upgrading to v1.83.1 removed it and all checks were repeated.
- `sh -n hack/e2e-kind.sh hack/setup-envtest.sh hack/setup-kind.sh` and
  `git diff --check` — passed.
- No real credentials, subscription URLs, generated proxy configuration, downloaded
  engine binary, cluster, image archive, local path or temporary fixture is tracked.

## Risks and deferred work

Native probe processes are bounded but expensive; reconciliation must avoid
unbounded worker occupancy. Kubernetes API publication proves stored desired bytes,
not BYO engine activation. Q10–Q12, managed runtimes, reload acknowledgment,
cross-namespace grants and HA remain deferred.

## Handoff

M6 completes the P0 milestone sequence on `feat/kubernetes-operator`. Release
hardening is next: Q12 must resolve engine redistribution, signing/SBOM, image
provenance, supported-version evidence and the private vulnerability channel before
a public runtime release. P1 design can then address managed runtimes, activation
acknowledgment, richer policy and HA without changing the completed M1–M6 core
boundaries implicitly.
