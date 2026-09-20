# Kubernetes BYO operator

M6 provides a namespace-scoped Kubernetes 1.37 baseline. Install one Helm release
per watched namespace. The operator publishes validated configuration but never
creates, restarts, exposes or confirms activation of Mihomo/sing-box workloads.

## Install

The image contains Mihomo v1.19.31 and sing-box v1.14.1 for native validation and
isolated probes. Public release provenance remains gated by Q12. Build a development
image with `make docker-build IMG=...`, push it to a trusted registry, and install
with an immutable digest when possible:

```sh
helm upgrade --install egressfox charts/egressfox \
  --namespace egressfox --create-namespace \
  --set image.repository=registry.example/egressfox \
  --set image.digest=sha256:REPLACE_WITH_VERIFIED_DIGEST
```

The chart installs the CRDs, one operator replica, a retained RWO PVC, ServiceAccount,
namespace-limited manager/leader bindings and authenticated HTTPS metrics. CRDs are
installed from `crds/`; Helm does not upgrade or delete them automatically. Review
generated CRD diffs and apply compatible upgrades before upgrading a release. The
PVC has Helm's `keep` annotation and requires explicit administrator cleanup.

The initial support claim is Kubernetes 1.37.0 with controller-runtime v0.25.1.
`make test-envtest` uses checksum-pinned 1.37.0 API-server assets and `make e2e-kind`
uses kind v0.33.0 with a digest-pinned Kubernetes 1.37.0 node image.
The kind script uses Docker by default; for Podman run
`KIND_EXPERIMENTAL_PROVIDER=podman CONTAINER_CLI=podman make e2e-kind`.

## API and Secret contracts

`ProxyPool` references one to 32 same-namespace Secret keys containing explicit
`URIList` or `Base64URIList` subscriptions and one Secret key containing an HTTP(S)
probe target. Source IDs are safe provenance labels. Strict whole-source admission
is default; `allowPartial`, `allowEmpty`, private-network and insecure-TLS choices
are explicit. M2's byte, record and record-length limits still apply.

`EgressGateway` references one pool, selects the pinned Mihomo or sing-box profile,
sets a loopback SOCKS listener and names its output Secret. It has no workload or
reload fields. The output Secret has type `egressfox.io/engine-config`, exact
Gateway controller ownership, `config.yaml` or `config.json`, and the protected
`.egressfox-receipt` data key. Every data value is confidential. Do not expose,
commit, diff or log the Secret.

An existing output name is accepted only when its type and controller owner UID
match exactly. Otherwise publication fails and any previous owned LKG remains.
The publisher rechecks the exact input object revisions before mutation and rejects
secret data above 900 KiB before contacting the output object. Identical desired
bytes are a no-op. Gateway deletion lets Kubernetes garbage
collection delete the output; input Secrets and BYO workloads are never modified.
Manual output deletion or drift is repaired on reconciliation after validation.
A deleted source Secret or ProxyPool makes dependent Conditions false while leaving
the last owned output Secret intact. Deleting a ProxyPool never cascades to a
Gateway. Deleting the Gateway explicitly releases its owned output.

Mount only the output key required by the chosen engine. The M6 compatibility
profiles and typical commands are:

| Engine | Secret key | Suggested mount | Command |
| --- | --- | --- | --- |
| Mihomo v1.19.31 | `config.yaml` | `/etc/egressfox/config.yaml` | `mihomo -f /etc/egressfox/config.yaml -d /var/lib/mihomo` |
| sing-box v1.14.1 | `config.json` | `/etc/egressfox/config.json` | `sing-box run -c /etc/egressfox/config.json -D /var/lib/sing-box` |

The engine's state directory is separate from the read-only Secret mount. Secret
volume propagation alone does not make a running engine reload its configuration;
the BYO workload owner must implement restart or a supported reload mechanism.
EgressFox does not observe that activation in M6.

## Status and operation

Pool status reports bounded admission counts, `lastInventoryChangeTime`,
`SourcesReady` and `Ready`.
Gateway status reports bounded eligible/selected counts plus `SelectionReady`,
`ConfigurationValid`, `Published` and `Ready`. Messages contain stable safe text.
`Published=True` means exact validated bytes are in the owned Secret; runtime
activation and traffic readiness remain unknown.

The single process uses leader election and serial SQLite access on the RWO PVC.
Missing state causes a conservative cold start and re-probing; the current output
Secret remains the Kubernetes LKG. Multiple replicas, shared/RWX SQLite, cross-
namespace references and HA are unsupported. The chart deliberately has no replica
setting. Probe work is capped at 64 deterministic endpoints per reconcile, with M4
global/per-target budgets; remaining endpoints advance on later refreshes. Gateway
decision scopes use object UIDs and inactive checkpoints expire after 30 days;
shared observations retain the existing age and count bounds.

Metrics use controller-runtime's authenticated HTTPS filter. Health endpoints are
served separately on port 8081. The ServiceAccount can read/write Secrets and read
CRs only in its release namespace, update CR status, and manage its Lease. It cannot
delete Secrets or mutate workloads.

## Validation and recovery

Use `make generate-check`, `make helm-check`, `make test-envtest`, and
`make e2e-kind`. The kind test checks install/upgrade, negative RBAC, a real VLESS
probe path, owned Secret output, BYO sing-box traffic and LKG preservation after a
bad credential rotation. It creates and deletes an isolated kind cluster.

If the PVC is lost, retain the output Secret and allow fresh probes to rebuild
evidence before publication. If the output Secret collides with an unrelated object,
choose a new name or remove the unrelated object deliberately; EgressFox will not
adopt it. If a CR must be removed while the operator is unavailable, no finalizer
blocks deletion.
