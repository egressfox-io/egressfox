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

`ProxyPool` references one to 32 same-namespace Secret keys containing explicit
`URIList` or `Base64URIList` subscriptions and one Secret key containing an HTTP(S)
probe target. Source IDs are safe provenance labels. Strict whole-source admission
is the default; `allowPartial`, `allowEmpty`, private-network and insecure-TLS
choices are explicit. Parser, record, byte, probe and scheduling bounds from P0
remain in force for both runtime modes.

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

Pool status reports bounded admission counts, `SourcesReady` and `Ready`. Gateway
status also reports selection/configuration state and the runtime Conditions above.
Messages are stable and credential-safe. A deleted source or Pool makes dependent
Conditions false while the last owned output/runtime remains untouched.

The operator uses one leader and serial SQLite access on the RWO PVC. State loss
causes conservative re-probing while Kubernetes-owned LKG resources remain. Multiple
operator replicas, shared SQLite, managed replicas greater than one, PDB/topology,
cross-namespace references, external Services, HTTP listeners, transparent routing,
traffic-level health and HA are not M7 features.

Controller-runtime metrics use authenticated HTTPS; health endpoints are separate
on port 8081. The ServiceAccount can read/write the required Secrets and manage only
Deployments, Services and NetworkPolicies in its release namespace, update EgressFox
status and manage its Lease. It cannot reach another namespace, Nodes, StatefulSets,
admission resources or arbitrary cluster networking. The managed engine Pod receives
no ServiceAccount token.

Use `make generate-check`, `make helm-check`, `make test-envtest`, and
`make e2e-kind`; `make k8s-compat` runs those Kubernetes layers across every
release-qualification profile. The kind test covers chart upgrade, negative namespace/cluster
RBAC, BYO regression, both managed engines, mandatory authentication, controlled
traffic, exact-generation rollout failure/LKG and recovery, bounded generations,
owned-resource repair, operator restart and both mode transitions.
