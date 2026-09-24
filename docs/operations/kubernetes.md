# Kubernetes operator

EgressFox runs one namespace-scoped operator per Helm release. A Gateway can be
explicitly managed, in which case the operator maintains an authenticated SOCKS5
Service backed by the selected pinned engine, or remain BYO and receive only a
validated configuration Secret. Runtime omission preserves the M6 BYO contract.

## Install and compatibility

The release image contains EgressFox, a fixed readiness helper, source-built Mihomo
1.19.31 and the branded sing-box-compatible 1.14.1 derivative. The image, source,
notices, SBOM, scan and provenance share the release contract in
[ADR 0012](../decisions/0012-release-distribution-and-provenance.md). After a release
exists, download its `egressfox-<version>.tgz` asset, verify it with the release's
`SHA256SUMS`, and install with the verified image digest. The command below uses
placeholders; no public artifact exists yet:

```sh
helm upgrade --install egressfox ./egressfox-0.1.0-dev.1.tgz \
  --namespace egressfox --create-namespace \
  --set image.repository=ghcr.io/egressfox-io/egressfox \
  --set image.digest=sha256:REPLACE_WITH_VERIFIED_DIGEST
```

The chart installs CRDs, one operator replica, a retained RWO PVC, ServiceAccount,
namespace-limited manager/leader bindings and authenticated HTTPS metrics. Helm
preserves CRDs and the annotated PVC on uninstall and does not upgrade files under
`crds/`. Apply a reviewed compatible CRD update before upgrading the chart. Alpha
schema additions have no conversion webhook or automatic migration.

The qualified Kubernetes profiles are 1.32, 1.34 and 1.37 with controller-runtime
v0.25.1. The default is the minimum 1.32 profile; select another with
`K8S_VERSION=1.34 make test-envtest helm-check e2e-kind`, or run
`make k8s-compat` before a release. See the authoritative
[Kubernetes compatibility contract](kubernetes-compatibility.md). For Podman use
`KIND_EXPERIMENTAL_PROVIDER=podman CONTAINER_CLI=podman make e2e-kind`.

`ProxyPool` has one to 32 sources. Each is either a same-namespace Secret key
containing a `URIList`, `Base64URIList`, `JSON` or conservatively detected `Auto`
subscription, or a managed HTTP source
whose URL and optional Authorization value come from same-namespace Secret keys.
The probe target also comes from a Secret key. Source IDs are safe provenance
labels. Strict whole-source admission is the default; `allowPartial`, `allowEmpty`,
HTTP/private source destinations, private probe destinations and insecure endpoint
TLS choices are separate. Parser, record, byte, probe and scheduling bounds remain
in force for both runtime modes.

## Managed HTTP sources

Keep the full URL in a Secret even when it appears public: subscription paths and
queries often contain tokens. If authentication is needed, put the complete HTTP
`Authorization` value in another Secret key. The following shape is illustrative;
replace the values through a secret-management path and use an authorized probe
target from the [samples](../../config/samples/README.md):

```yaml
apiVersion: v1
kind: Secret
metadata: {name: subscription-access}
stringData:
  url: https://subscriptions.example.test/replace-me
  authorization: Bearer replace-me
---
apiVersion: egressfox.io/v1alpha1
kind: ProxyPool
metadata: {name: external-api}
spec:
  sources:
    - id: provider-a
      http:
        urlSecretRef: {name: subscription-access, key: url}
        authorizationSecretRef: {name: subscription-access, key: authorization}
        maxStale: 24h
      format: Auto
  probe:
    targetSecretRef: {name: probe-target, key: url}
  refreshInterval: 5m
```

Exactly one of `secretRef` and `http` is required per source. Mixed sources are
supported. Existing Secret sources need no migration. HTTPS and public destination
addresses are the default. Plain HTTP requires `http.allowHTTP: true`; ordinary
RFC1918/IPv6 ULA and in-cluster destinations require
`http.allowPrivateNetworks: true`. Loopback independently requires
`http.allowLoopback: true`; link-local, metadata, multicast, unspecified and
reserved/control destinations remain prohibited. DNS answers are authorized at
the actual dial, including retries and same-origin redirects.

The known AWS IPv6 metadata address `fd00:ec2::254` is denied even with private
sources enabled. The application checks known metadata destinations; retain
cluster egress policy to cover provider-specific destinations outside that list.

Source TLS certificate verification is enabled
by default and can be disabled only with a separate trusted
`http.allowInsecureTLS: true` opt-in. These source-fetch flags do not authorize
probe targets or endpoint addresses. Existing format values and admission flags
remain valid. The additive `JSON` and `Auto` formats, Default/Custom/Clean HTTP
profiles and supported endpoint variants are defined in the
[subscription compatibility matrix](../designs/subscription-compatibility.md).

The automatic HWID belongs to a logical subscription, not its URL or cache entry.
Existing sources keep their exact Pool UID/source ID based HWID after upgrade. To
rename `provider-a`, first set `http.clientIdentity` to
`legacy:<current-Pool-UID>/provider-a`, apply it, then change the source ID. Keep
that reference when moving the source to another Pool. For a new portable source,
assign a unique `stable:<opaque>` reference before its first refresh. A user-set
`profile.hwid` overrides the automatic value; removing the override restores it.
Changing `clientIdentity` deliberately rotates the automatic value. A deleted
source recreated without its previous reference may receive a new HWID; preserve
the reference or a fixed override when migrating clusters. HWID continuity does
not preserve incompatible cache bytes or ETags. Format or request-context changes
cause an unconditional refresh, while an already healthy published Gateway remains
available if that refresh fails.

The pool `refreshInterval` (30 seconds to 24 hours, default five minutes) schedules
conditional refresh. The HTTP source's `maxStale` (one minute to seven days,
default 24 hours) bounds fallback from the last successful remote validation.
The operator sends ETag and Last-Modified only with a matching intact cache.
A 304 renews validation time without replacing body bytes. A failed fetch or
malformed response leaves the accepted cache intact; at expiry its endpoints stop
contributing to current inventory. This does not delete an already published
Gateway generation or stop a healthy managed runtime. New accepted bytes that
render the same validated artifact do not cause a rollout.

The Pool controller schedules its next reconcile no later than the earliest
currently admitted HTTP cache expiry, even when that precedes `refreshInterval`.
At expiry it recomputes per-source status and effective inventory without a new
provider response; an already expired cache does not cause immediate requeues.

Refresh uses four leader-scoped workers, no more than three attempts and a
45-second overall deadline per source. Temporary network/timeout, 429 and 5xx
failures retry with bounded jitter/backoff; configuration, destination and
parse/admission failures do not. Restart restores compatible unexpired cache from
the protected SQLite PVC. Changing URL or Authorization bytes, source variant,
format, admission or HTTP authorization flags invalidates compatibility. A
metadata-only Secret edit does not. Removed sources are pruned from protected
storage; do not treat the retained PVC or its backups as non-sensitive.

`status.sources[]` reports safe source ID, `Fresh`, `Cached`, `Expired` or
`Unavailable`, a bounded reason and last successful validation time.
`SourcesReady=False` with `Ready=True` means a usable inventory remains despite a
degraded source. `Ready=False` after cache expiry means no current inventory was
admitted. The status never includes URL, Authorization, validators, source bytes
or a digest. Operators should check Gateway `RuntimeReady` separately from source
freshness. Arbitrary request headers, cross-namespace refs,
provider quota handling and HA refresh are unsupported.

## Minimal managed Gateway

Create the same-namespace subscription and target Secrets described in the
[samples](../../config/samples/README.md), then opt in explicitly:

```yaml
apiVersion: egressfox.io/v1alpha1
kind: EgressGateway
metadata:
  name: external-api
spec:
  poolRef: {name: external-api}
  engine: SingBox
  runtime:
    managed: {}
```

Managed mode deliberately has no image, replica, Pod-template, listener, Service
type or arbitrary environment knobs. It always creates one replica with an
authenticated SOCKS5 listener on port 1080 behind a ClusterIP Service. The selected
engine, renderer, native validator and executable all come from the same exact
release profile; a Gateway cannot inject a different image.

Discover the Service and credential Secret only after status reports them:

```sh
gateway=external-api
namespace=egressfox
kubectl -n "$namespace" describe egressgateway "$gateway"
service=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.serviceName}')
auth=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.clientAuthSecretName}')
username=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "username" | base64decode}}')
password=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "password" | base64decode}}')
printf 'SOCKS endpoint: %s:1080; username: %s\n' "$service" "$username"
```

Do not print the password in normal automation or logs. A workload must read the
same-namespace `kubernetes.io/basic-auth` Secret through its own least-privilege
delivery path and explicitly configure `socks5h://username:password@SERVICE:1080`.
EgressFox does not inject workloads or transparently redirect traffic.
Check the `Published`, `Activated`, `RuntimeReady` and `Degraded` Conditions before
connecting a client. The Service DNS name is `$service.$namespace.svc` from another
namespace.

The generated username is `egressfox`; the 32-byte random password is stable for
the Gateway lifetime. M7 has no user-provided, scheduled or in-place credential
rotation. Deleting the client auth Secret repairs it from protected active
generation data with the same credential, avoiding a client/runtime mismatch.
Deleting both the auth Secret and all recoverable generation inputs can require a
new credential and rollout; deliberate zero-downtime rotation is later scope.

## Managed resources and security

One managed Gateway exclusively owns:

- one immutable client-auth Secret;
- the current published, active and immediately previous immutable configuration
  generations, plus any generation still referenced by a non-terminal Pod;
- one single-replica Deployment;
- one ClusterIP Service exposing only TCP `socks` port 1080; and
- one ingress-only NetworkPolicy allowing namespace-local access to that port.

Every child has an exact controller OwnerReference. A same-named unrelated object
causes a safe ownership conflict and is never adopted or overwritten. Deleting the
Gateway relies on Kubernetes garbage collection; no network-dependent finalizer is
used. Manually deleted exact-owned children are reconciled back when their inputs
remain valid.

The engine Pod runs as UID/GID 65532, non-root, with a read-only root filesystem,
all capabilities dropped, privilege escalation disabled, RuntimeDefault seccomp,
no service-account token, no host namespace and only bounded state/temp `emptyDir`
volumes. It starts the fixed engine executable directly, without a shell, network
download or control API. Credentials and config are mounted files and never appear
in status, labels, annotations, args or normal diagnostics.

The NetworkPolicy is defense in depth and requires a CNI that enforces it. Namespace
peers still need SOCKS credentials. Administrators must treat namespace Pod creation,
Secret read/mount permission, etcd encryption and node access as trust boundaries.

## Generations, activation and readiness

A distinct validated artifact becomes a new immutable Secret with a random opaque
name. The name is the public generation identity; it is not derived from the
secret-bearing content or receipt. Identical artifact and auth inputs reuse the
existing generation, so an unchanged reconcile creates neither a Secret nor a
rollout. Protected receipt and credentials remain Secret data.

Status separates these facts:

| Condition/field | Exact meaning |
| --- | --- |
| `Published=True` / `publishedGeneration` | Exact native-validated bytes exist in that owned immutable generation Secret |
| `Activated=True` / `activeGeneration` | The Deployment observed the desired template; its one updated replica is Ready/available and no old replica remains for that rollout |
| `RuntimeReady=True` | At least one managed Pod behind the stable Service is Ready; during a failed replacement it can be the old active generation |
| `Degraded=True` | Current desired activation or runtime ownership failed; inspect its safe reason/message |

Readiness executes a small fixed helper that reads mounted credentials, connects to
loopback and completes SOCKS5 username/password negotiation with the engine. It does
not forward a request. Thus readiness proves that the intended authenticated
listener accepted the configured credentials; it does not prove destination,
endpoint or application traffic health. M7 intentionally has no liveness probe or
engine-specific live reload.

A normal rollout mounts the new immutable Secret in a new Pod. Deployment strategy
`maxUnavailable: 0`, `maxSurge: 1` keeps the old Ready Pod available until the new
one becomes Ready. The Service uses one stable ownership selector and Kubernetes
readiness, avoiding a generation-switch gap. Once the desired generation is fully
active, status advances and safe bounded cleanup runs.

If replacement startup/readiness fails, `Published=True`, `Activated=False` and
`Degraded=True`; `RuntimeReady=True` may truthfully report that the previous LKG is
still serving. The 120-second Deployment progress deadline bounds activation. The
controller neither destroys the old ready generation nor turns a process failure
into negative endpoint evidence. Correct the desired inputs or capacity constraint
to recover.

## BYO compatibility and mode transitions

An omitted `runtime` remains BYO and requires the existing `outputSecretName`:

```yaml
spec:
  poolRef: {name: external-api}
  engine: SingBox
  listener: {address: 127.0.0.1, port: 1080}
  outputSecretName: external-api-engine-config
```

Its owned Secret has type `egressfox.io/engine-config`, contains `config.json` or
`config.yaml` plus protected `.egressfox-receipt`, and keeps M6 owner/conflict,
snapshot and LKG semantics. `Published=True` does not mean the user's runtime loaded
it; `Activated` and `RuntimeReady` remain `Unknown`. The BYO owner must mount only
the engine key and arrange restart/reload. Suggested exact-profile commands are:

| Engine | Secret key | Suggested mount | Command |
| --- | --- | --- | --- |
| Mihomo 1.19.31 | `config.yaml` | `/etc/egressfox/config.yaml` | `mihomo -f /etc/egressfox/config.yaml -d /var/lib/mihomo` |
| sing-box-compatible 1.14.1 | `config.json` | `/etc/egressfox/config.json` | `sing-box run -c /etc/egressfox/config.json -D /var/lib/sing-box` |

BYO to managed first publishes and activates the managed generation, then removes
only the exact-owned old BYO output Secret. It never adopts or deletes a user's
workload. Managed to BYO first publishes the requested BYO output, then deletes only
the exact-owned Deployment, Service, NetworkPolicy, auth and generation Secrets and
clears managed status. An older M6 operator cannot parse the new runtime field;
switch objects back to BYO and complete cleanup before a binary downgrade.

## Operation and recovery

Pool status reports bounded admission counts, per-source freshness,
`SourcesReady` and `Ready`. Gateway
status also reports selection/configuration state and the runtime Conditions above.
Messages are stable and credential-safe. A deleted source or Pool makes dependent
Conditions false while the last owned output/runtime remains untouched.

The operator uses one leader and serial SQLite access on the RWO PVC. State loss
causes conservative HTTP refetch and re-probing while Kubernetes-owned LKG resources
remain. Multiple
operator replicas, shared SQLite, managed replicas greater than one, PDB/topology,
cross-namespace references, external Services, HTTP listeners, transparent routing,
traffic-level health and HA are not current features.

Controller-runtime metrics use authenticated HTTPS; health endpoints are separate
on port 8081. The ServiceAccount can read/write the required Secrets and manage only
Deployments, Services and NetworkPolicies in its release namespace, update EgressFox
status and manage its Lease. It cannot reach another namespace, Nodes, StatefulSets,
admission resources or arbitrary cluster networking. The managed engine Pod receives
no ServiceAccount token.

Use `make generate-check`, `make helm-check`, `make test-envtest`, and
`make e2e-kind`; `make k8s-compat` runs those Kubernetes layers across every
release-qualification profile. The kind test covers chart upgrade, negative
namespace/cluster RBAC, BYO regression, both managed engines, authentication,
controlled traffic, exact-generation rollout failure/LKG and recovery, bounded
generations, owned-resource repair, operator restart, mode transitions and a
controlled HTTP subscription with conditional/no-op, fallback, recovery and expiry.
