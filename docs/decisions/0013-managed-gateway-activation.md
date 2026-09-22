# ADR 0013: Managed gateway ownership and exact-generation activation

Date: 2026-09-22. Status: accepted.

## Context

M6 publishes one validated, secret-bearing engine configuration but deliberately
does not own a runtime. M7 must turn that output into an authenticated in-namespace
SOCKS Service without changing existing objects into managed workloads, exposing
engine control APIs, or treating Secret publication as process health. This resolves
Q7's managed-activation remainder and Q13.

Both pinned profiles have a common username/password SOCKS5 inbound. Mihomo
v1.19.31 accepts global `socks-port`, `allow-lan`, `bind-address` and
`authentication` settings; sing-box v1.14.1 accepts a SOCKS inbound with `listen`,
`listen_port` and `users`. The evidence is the source and documentation at the
exact release-manifest commits: [Mihomo configuration](https://github.com/MetaCubeX/mihomo/blob/ab405bad5beeeac8b003bb01f60f134f6df54471/docs/config.yaml)
and [sing-box SOCKS inbound](https://github.com/SagerNet/sing-box/blob/1ac1a339cb1223e9c70eae14c44411c75033c02d/docs/configuration/inbound/socks.md).
Kubernetes Deployments can preserve an old ready Pod while a replacement is
unready when rolling update uses `maxUnavailable: 0` and `maxSurge: 1`; immutable
Secrets give each Pod template a stable configuration input.

## Decision

### API and compatibility

`egressfox.io/v1alpha1` remains the API version. `EgressGateway.spec.runtime` is
an optional discriminated union whose M7 member is `managed: {}`. Omission means
BYO exactly as it did in M6. BYO requires `outputSecretName` and retains its
loopback `listener`; managed mode forbids those BYO-only fields and owns a fixed
SOCKS port 1080. Admission validation rejects an empty runtime object and mixed
managed/BYO intent. Downgrading an object with the new field requires first
switching it to BYO; an M6 binary is not expected to understand M7 fields.

### Listener and credentials

Managed mode renders one username/password-authenticated SOCKS5 listener on
`0.0.0.0:1080`. It does not render HTTP, mixed, control, TUN or host listeners.
The operator creates one immutable `kubernetes.io/basic-auth` Secret per Gateway,
with fixed username `egressfox` and a cryptographically random 32-byte base64url
password. Credentials never enter spec, status, args, labels, annotations or logs.
Deleting that owned Secret is the explicit M7 rotation operation: the operator
generates a replacement and the changed auth input creates a new runtime generation.
User-provided credentials and automatic scheduled rotation are deferred.

### Image authority and process

The managed Pod uses the exact EgressFox release image configured for the operator,
not a per-Gateway image. That signed image already contains the source-built,
profile-bound Mihomo 1.19.31 and branded sing-box-compatible 1.14.1 binaries covered
by ADR 0012, SBOM, notices, vulnerability scanning and provenance. Fixed executable
paths and argument arrays select only the requested engine. The Pod has no API
token, shell, engine control port, host namespace or elevated capability. Dedicated
engine-only images may reduce future attack surface, but adding another signed
artifact contract is not required for M7.

### Owned resources

Managed mode owns exactly:

- one immutable client-auth Secret;
- bounded immutable generation Secrets containing config plus protected receipt;
- one single-replica Deployment;
- one ClusterIP Service exposing only TCP port 1080; and
- one ingress-only NetworkPolicy allowing namespace-local access to that port.

Every object has an exact controller OwnerReference. A same-named unowned or
differently typed object is a conflict and is never adopted. Owner-reference garbage
collection handles Gateway deletion; no finalizer is added. Switching managed to
BYO deletes only exact-owned managed children after BYO publication is available,
never a user workload. Switching BYO to managed leaves the old owned BYO output
Secret intact until the managed generation activates, then removes only that owned
output. User-owned resources are never deleted.

### Generation and rollout

Every distinct validated managed artifact is stored in a new immutable Secret.
The API server assigns an opaque random name from a Gateway-derived `generateName`;
the public generation identifier is that non-content-derived name. The protected
receipt remains Secret data and no artifact digest appears in metadata or status.
An identical artifact/auth input reuses the existing owned generation Secret.

The Deployment template names the exact generation Secret and carries only the
opaque generation identifier. It uses one replica, `RollingUpdate`,
`maxUnavailable: 0`, `maxSurge: 1`, and a bounded progress deadline. A stable
Service selector spans old and new ReplicaSets, while endpoint readiness prevents
traffic to an unready replacement. The old ready Pod and generation Secret remain
while a replacement is unresolved. After exact-generation activation, cleanup
retains the active and immediately previous generation plus every generation still
referenced by a non-terminal Pod; other owned generation Secrets are deleted.

### Activation and readiness

The conditions have deliberately separate meanings:

- `Published=True`: exact validated bytes and protected receipt exist in the
  current owned immutable generation Secret.
- `Activated=True`: the Deployment controller has observed the desired template,
  exactly one updated replica is ready/available, and no old replica remains
  serving for that rollout. The template references the published generation.
- `RuntimeReady=True`: at least one managed Pod behind the stable Service is Ready.
  During a failed replacement this can describe the previous active LKG while
  `Activated=False` for the desired generation and `Degraded=True`.

Readiness is an exec probe implemented by a small fixed health helper in the release
image. It reads credentials from mounted files, connects only to loopback, negotiates
SOCKS5 username/password authentication, and exits without proxying application
traffic. Credentials do not appear in Pod args or process listings. This proves the
intended authenticated listener accepted the configured credentials; it does not
prove arbitrary destinations, selected endpoints or application requests work.
No liveness probe is added in M7.

Deployment `ProgressDeadlineExceeded` or the equivalent bounded timeout makes the
desired generation degraded and not activated. Kubernetes keeps the old ready Pod
because availability cannot drop below one. EgressFox does not synthesize negative
endpoint observations or implement a separate history manager. Correcting relevant
desired input starts/reuses a new rollout. Reconciliation remains periodic but does
not create a generation, rotate credentials or patch a Deployment when inputs and
owned state are unchanged.

## Alternatives rejected

- Omitted runtime meaning managed: breaks M6 upgrade safety.
- Arbitrary image, Pod template or Service type fields: lose the tested profile and
  security contract and begin later P1 scope.
- Unauthenticated SOCKS plus NetworkPolicy: CNI enforcement is optional and is not
  application authentication.
- Engine reload/control APIs: the exact engines have different mechanisms and no
  common atomic acknowledgment contract.
- Mutable configuration Secret: projected updates do not prove which bytes a
  process loaded and make rollback attribution ambiguous.
- Artifact-derived public generation hashes: disclose equality and invite guessing
  against secret-bearing low-entropy inputs.
- A dedicated engine image in M7: useful hardening, but creates a second release
  artifact/signing/provenance contract without improving version binding over the
  current combined image.

## Consequences

Managed mode temporarily runs two engine Pods during rollout and needs capacity for
the surge. Namespace peers can reach the Service but still need credentials;
NetworkPolicy enforcement depends on the CNI. Anyone able to create Pods in the
namespace may be able to mount readable Secrets under ordinary Kubernetes
authorization, so namespace tenancy and encryption at rest remain administrator
boundaries. A manually deleted previous-generation Secret cannot be reconstructed
after its bytes cease to be current desired state; a running Pod keeps its mounted
copy, but replacement safety is degraded and is reported. M8 and later milestones
do not change this M7 activation contract.
