# Kubernetes API and operator

Status: M6 namespace-scoped P0 API, controllers, generated manifests and Helm chart
implemented. [ADR 0011](../decisions/0011-namespaced-byo-operator.md) owns the
concrete topology and Secret contract; later sections retain P1 design constraints.
[ADR 0003](../decisions/0003-operator-tooling.md) accepts Kubebuilder/controller-runtime
and an initial alpha API, while deferring generation until the M6 design gate.

## Responsibilities and relationships

M6 implements namespaced `ProxyPool` and `EgressGateway` resources in
`egressfox.io/v1alpha1` for P0; `EgressPolicy` is P1. Names, field structures, and
defaults are not frozen. Use the following relationships to evaluate an API;
this table is not an installable schema.

| Resource | Desired responsibility | Status responsibility | Must not own |
| --- | --- | --- | --- |
| ProxyPool | Secret source references, admission flags, probe authorization, selection strategy/Top-N and refresh interval | Bounded accepted/rejected/unsupported counts, last inventory-change time, Conditions | Gateway workload lifecycle or raw credentials/history |
| EgressGateway | Pool reference, pinned engine profile, loopback SOCKS listener and owned output Secret name | Bounded eligible/selected counts, last publication time, observedGeneration and Conditions | Fetching independently, exposing artifact revisions, or mutating user-managed engines |
| EgressPolicy (P1) | Routing intent associated with a gateway | Acceptance/conflict status for that policy | A separate connection-level router |

One pool can feed multiple gateways. P0 uses one pool reference per gateway; multiple
pools, routing rules and standalone EgressPolicy composition belong to P1. M6 renders
the M3 minimal loopback SOCKS gateway only and does not expose a second routing model.

P0 references are same-namespace, with explicit names and Secret keys;
cross-namespace grants and multi-tenancy are deferred. Source credentials belong
in Secrets, not CR spec strings, annotations, Events, or status. Public source URLs
still need a secret-reference option because paths/query parameters may hold tokens.
Reject missing references, unresolved ownership, invalid bounds, and unsupported
engine profiles clearly. Secret rotation must trigger reconciliation.

Do not use a separate CRD for every normalized endpoint or observation. The API
server is not a time-series database. If pool inventory needs durable transport
between reconcilers, choose a private bounded store or shared application service;
do not hide credential-bearing endpoints in status.

## Spec, status, and compatibility

Follow [Kubernetes API conventions](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-architecture/api-conventions.md):
spec expresses intent; status reports observations through a status subresource.
Use standard `metav1.Condition`, a map-list keyed by type, bounded reason codes,
sanitized messages, and meaningful transition times. Do not update status on every
probe or change timestamps on a no-op reconcile.

Potential conditions are `Ready`, `SourcesReady`, `ProbeDataReady`,
`SelectionReady`, `ConfigurationValid`, and `Published`; choose conditions per
resource and define their truth tables before implementation. Missing/unknown
conditions must not be treated as success. `Published=True` is not evidence of
runtime activation; unmanaged gateway traffic readiness is unobservable by default.

M6 Pool status reports deduplicated accepted endpoints plus rejected and unsupported
source records. Gateway status reports eligible and selected endpoint counts. It
also reports the last inventory-change/publication times. Neither status exposes an
artifact reference. Do not expose endpoint lists, credentials, secret-derived
digests, free-form errors, or unbounded decision/history arrays.

`observedGeneration` indicates the spec generation a status/Condition evaluates;
it does not imply successful reconciliation. A Secret update or new pool snapshot
can change desired output without changing a gateway's `metadata.generation`.
Track dependency input revisions separately using a safe representation, and
never set success for an obsolete combination of inputs.

Use structural schemas, explicit enums only for implemented strategies, documented
units, bounded lists/strings, and validation of cross-field constraints. Prefer
schema/CEL validation and ordinary defaulting where sufficient; no admission webhook
just to carry boilerplate. Distinguish omitted, zero, and disabled semantics before
choosing pointers/defaults. Defaults affecting safety must be visible and tested.

Alpha permits evolution, not careless data loss. Every public field change needs
compatibility review: stored objects, defaults, unknown fields, conversion/storage
version, examples, and migration/rollback. Do not add a `v1beta1` until behavior is
supported and upgrade tests justify it. Core types must remain free of Kubernetes
serialization tags and API machinery.

## Reconciliation ownership

Use focused reconcilers, consistent with [Kubebuilder guidance](https://book.kubebuilder.io/reference/good-practices):

- ProxyPool reconciliation validates desired inputs and submits/coordinates pool
  refresh and scheduled probe work through the shared core services. Long network
  sweeps must not occupy an API reconcile worker indefinitely.
- EgressGateway reconciliation consumes an immutable pool decision and current
  gateway intent, then renders, validates, and publishes through shared use cases.
- A future EgressPolicy reconciler owns policy acceptance/composition, with an
  explicit conflict and precedence model. It is not scaffolded in P0.

Watch the primary resource, relevant Secret/input references, owned output objects,
and pool revision changes using indexed reverse references. Use scoped caches or
targeted reads so watching one Secret does not require all-cluster secret visibility.
Probe/refresh timers and internal completion signals must enqueue affected resources;
spec generation changes alone are insufficient. Avoid broad status-driven loops.

Reconcile from current state, tolerate duplicate/missed events, use bounded context
deadlines and rate-limited retries, and handle resourceVersion conflicts by reading
again. Permanent invalid intent should set a Condition and wait for relevant change,
not spin. Avoid destructive writes before all validation succeeds.

P0 must guard the active scheduler/publisher with leader election and a documented
single-active-process topology. Lease ownership alone is not storage fencing and
does not prevent a stalled old writer from completing work after losing leadership.
Cancel leader-scoped work, recheck target revisions/ownership before commits, and
resolve the state/write-fencing contract before claiming multiple-replica support.

## Persistence and topology gate

Standalone SQLite does not settle operator storage. Proposed P0 deployment is a
single active operator with durable local-volume-backed history and serialized
writes, with conservative restart behavior. Before M6 code, choose and validate:

1. Where inventory, observations, anti-flapping state, and publication receipts live.
2. How reconcilers get coherent pool revisions without using CR status as a database.
3. Volume attachment, operator replacement, database ownership, and restart recovery.
4. Behavior when state is missing/unavailable and how old LKG output is discovered.
5. Leader-scoped jobs, storage locking, and cancellation under leadership loss.

A shared SQLite database on an RWX/network filesystem is not an acceptable HA
shortcut. P1 operator HA requires its own supported state/coordination design;
PostgreSQL remains a P2 idea unless priorities are deliberately revised. This is
an acknowledged roadmap dependency, not a promise that HA is possible with no
storage work. No unsupported multi-replica Helm value should be exposed in P0.

## Output ownership and deletion

A Gateway may own only resources it intentionally creates. The pool, gateway,
and later policies reference one another without automatic ownership: deleting a
pool must not garbage-collect user gateways. Never adopt an existing unrelated
Secret/Deployment based only on matching its name. Update only managed keys/fields,
reject ownership collisions, and define release/retention semantics.

Output deletion is consequential: retaining credentials and preserving connectivity
pull in different directions. M6 uses an ordinary Gateway controller OwnerReference.
Gateway deletion therefore makes the Secret eligible for garbage collection; manual
Secret deletion is repaired from a newly validated current generation. Referenced
input Secrets and BYO workloads are never deleted or altered.

Use owner references for ordinary owned Kubernetes resources. Add finalizers only
for a real cleanup obligation the API server cannot fulfill, with retry limits,
operator-unavailable recovery guidance, and a documented escape procedure. Never
add a finalizer just because a resource exists. Do not block deletion on every
network endpoint becoming reachable.

## Runtime modes and installation

P0 is BYO: generate/publish config while the user manages engine workload, proxy
exposure, config consumption, and reload. It is not an automatically configured
cluster egress path. Explicit application proxy use comes first.

P1 optional managed runtimes may own Deployments, Services, Secrets, credential-free
ConfigMaps, ServiceAccounts, and PodDisruptionBudgets. Resolve configuration reload,
activation, image versions/digests, ownership, resources, and rollout readiness
before managing them. A PDB does not make a single replica highly available.

Use least-privilege RBAC and a namespace-scoped initial installation where practical;
installation permissions for CRDs differ from runtime permissions. No wildcard
Secret access, workload mutation, privileged Pod, host network, NET_ADMIN, TUN,
iptables/nftables, or admission permissions for a configuration-only operator.
Non-root execution, read-only root filesystem with dedicated writable state/temp
volumes, dropped capabilities, resource bounds, and secure TLS are planned defaults.

### P1 API and runtime direction

The [P1 roadmap](../roadmap/p1.md) is authoritative for sequencing. M7 adds an
explicit managed runtime mode while omission remains the existing BYO behavior. It
owns a single-replica engine Deployment, authenticated SOCKS ClusterIP Service and
revision-bound generation Secret; it does not adopt user workloads. Exact fields,
client credential ownership, image authority, Secret retention and deletion are Q13
and require an ADR before CRD generation.

Managed status separates `Published`, `Activated` and `RuntimeReady`. The common
first activation path is a fresh engine process in a changed Pod template, not an
engine-specific live reload. Readiness establishes process/listener availability
for the exact Pod generation, not destination traffic health. BYO activation stays
unknown.

M9 evolves `ProxyPool` toward bounded named probe/selection profiles over one source
inventory while preserving the current fields as a legacy/default profile. M10 adds
`EgressPolicy` for routing intent only: a Gateway references at most one policy, and
the policy has no workload selector. Q15 and Q16 own exact migration and schema
choices. Cross-namespace grants, operator HA and transparent attachment are not P1.

Helm is P0 delivery work after generated API/controller behavior exists, not an
empty chart now. Chart upgrades must explicitly manage CRD evolution; validate a
clean install, upgrade, deletion/retention behavior, and RBAC in kind. Keep generated
CRDs authoritative and detect chart drift.

## Scaffolding gate and sources

At research time Kubebuilder [v4.16.0](https://github.com/kubernetes-sigs/kubebuilder/releases/tag/v4.16.0)
generates a sample using Go 1.26, controller-runtime 0.25.0, and Kubernetes modules
0.37.0. controller-runtime [compatibility guidance](https://github.com/kubernetes-sigs/controller-runtime#compatibility)
ties its minor version to the Kubernetes libraries; its current release observed
was 0.25.1. The [Kubebuilder compatibility policy](https://book.kubebuilder.io/versions_compatibility_supportability)
recommends the generated dependency/tool set. M6 retained this supported family.

M6 used checksum-verified Kubebuilder v4.16.0 in a disposable directory before
integrating its namespace-scoped Go v4 scaffold. `PROJECT` records the CLI; generated
deep-copy code, CRDs and RBAC are reproduced by `make generate-check` with
controller-tools v0.22.0. Pinned Kubernetes 1.37 envtest and kind evidence define
the initial support baseline.

## Non-goals and open questions

No frozen YAML API, EgressPolicy in P0, multi-tenancy, automatic workload routing,
transparent interception, or operator HA implementation. The
[ADR 0011](../decisions/0011-namespaced-byo-operator.md) owns the P0 API, topology,
deletion, RBAC and state decisions. P1 requires a new decision before managed
runtimes. Q15/Q16 gate profile/policy APIs; cross-namespace use and HA are deferred
beyond the committed P1 path.
