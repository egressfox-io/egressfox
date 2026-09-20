# ADR 0011: Namespace-scoped BYO operator and owned Secret publication

Date: 2026-09-20. Status: accepted.

## Context

M6 must expose the M1–M5 pipeline through Kubernetes without turning EgressFox
into a data plane. The API must keep credentials out of specs and status, preserve
last-known-good output across validation failures and restarts, and avoid granting a
configuration-only operator cluster-wide Secret access. SQLite is deliberately a
single-writer local database and cannot be placed on a shared network filesystem to
claim highly available operation.

Kubebuilder v4.16.0 was reviewed and checksum-verified on 2026-09-20. Its Go v4
scaffold targets Kubernetes 1.37.0, controller-runtime v0.25.0 and controller-tools
v0.22.0. The controller-runtime v0.25.1 patch is used without changing that supported
minor-version relationship. Kubernetes 1.37 envtest and kind form the initial tested
API-server baseline; this is not a promise that every older cluster is supported.

## Decision

Introduce namespaced `ProxyPool` and `EgressGateway` resources in
`controlplane.egressfox.io/v1alpha1`. A ProxyPool names a bounded list of same-
namespace Secret keys containing explicit URI-list or Base64 URI-list snapshots.
It also carries the probe target Secret reference and bounded selection settings.
An EgressGateway names one pool, one pinned engine profile, a loopback SOCKS listener
and an output Secret name. P0 has no EgressPolicy and no native engine fragments.

The operator is installed for exactly one namespace. Its cache and Role are scoped
to that namespace; source and target Secret references cannot cross it. The manager
uses leader election, but the chart fixes replicas to one and uses one RWO PVC for
the protected SQLite database. Lease election limits active work but is not storage
fencing, so multi-replica/HA operation is unsupported. Reconciliation contexts and
all child probe processes are cancelled when the elected manager stops.

ProxyPool reconciliation admits current Secret snapshots transactionally through
the M2 pipeline and reports only safe bounded counts and Conditions. Immutable
in-memory pool revisions are an optimization, not durable truth: a Gateway can
rebuild the referenced pool from current Secrets after restart. Observations and
M5 decision checkpoints remain in the protected SQLite store. Missing or corrupt
state causes a conservative cold start; it never makes unknown endpoints eligible.

Gateway reconciliation uses the current pool revision, M4 evidence, M5 selection,
M3 rendering and mandatory native validation. Its output adapter accepts only a
validated artifact. It creates a Secret of type `egressfox.io/engine-config`, owned
by that exact Gateway UID, with one engine configuration key and a protected receipt
key in `data`. Both are confidential. Safe labels describe ownership and engine;
artifact digests, endpoint IDs and credentials do not appear in metadata, status,
Events or logs.

The publisher refuses an existing Secret unless it has the exact expected controller
owner and managed type. It applies one resourceVersion-guarded replacement after
validation, reads the object back, and exposes its receipt to the M5 checkpoint
protocol. An identical configuration and receipt is a no-op. Kubernetes updates are
atomic at the API-object level; durability is the API server's responsibility.
There is no claim that a BYO engine consumed or activated the Secret.

The output Secret follows ordinary owner-reference garbage collection when its
Gateway is deleted. No finalizer is added: no external resource needs synchronous
cleanup, and Kubernetes envtest intentionally has no garbage-collection controller,
so tests verify ownership rather than pretending to verify GC. Referenced input
Secrets and BYO workloads are never modified or deleted. Manual output deletion is
repaired; manual mutation is replaced only after the next desired artifact passes
validation. Collision and validation failures preserve the previous owned Secret.

Controller watches use field indexes for Secret and pool references. Secret changes
map only to referencing pools and gateways, pool changes map only to referencing
gateways, and owned output Secret changes enqueue their owner. Reconciliation is
bounded and idempotent. Status updates use standard Conditions and observedGeneration;
`Published=True` means the desired validated bytes are in the owned Secret, not that
traffic is ready.

The image includes the exact Mihomo v1.19.31 and sing-box v1.14.1 binaries required
by ADR 0008. Downloads are version- and checksum-pinned per platform. Q12 still gates
public redistribution and release provenance; development images are not a public
runtime release.

## Alternatives

A cluster-scoped controller would simplify one-install-per-cluster operation but
would need broad Secret visibility. Storing inventories or artifact digests in CR
status would expose confidential material and misuse the API server as a database.
A ConfigMap cannot protect rendered credentials. A shared SQLite volume plus several
replicas has no reliable fencing. A finalizer for an ordinary owned Secret would make
deletion less recoverable without protecting an external obligation.

## Consequences

Each watched namespace needs its own EgressFox release and PVC. Output Secrets can
be mounted into user-managed engines, while reload and activation remain the user's
responsibility. A lost PVC loses evidence and anti-flapping state but not the current
owned LKG Secret; the operator probes again before changing it. P1 may design managed
runtimes, reload acknowledgment, cross-namespace grants and real HA, but cannot
obtain them by increasing this chart's replica count.

This resolves Q7's Secret-publication portion, Q8 and Q9. It follows the official
[Kubernetes API conventions](https://github.com/kubernetes/community/blob/main/contributors/devel/sig-architecture/api-conventions.md),
[Kubebuilder v4.16.0 release](https://github.com/kubernetes-sigs/kubebuilder/releases/tag/v4.16.0),
[controller-runtime v0.25.1 release](https://github.com/kubernetes-sigs/controller-runtime/releases/tag/v0.25.1),
and [envtest guidance](https://book.kubebuilder.io/reference/envtest).
