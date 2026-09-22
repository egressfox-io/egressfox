# Kubernetes compatibility

[ADR 0014](../decisions/0014-development-versioning-and-kubernetes-compatibility.md)
defines terminology and pinned inputs. This page is the authoritative EgressFox
compatibility claim; [the operator guide](kubernetes.md) owns installation and
runtime operation.

## Contract

EgressFox has a minimum supported Kubernetes minor of 1.32. Release qualification
covers the following exact profiles, using controller-runtime v0.25.1 and the
Kubernetes v0.37 Go libraries already pinned by the module:

| Kubernetes minor | envtest | kind node image | Qualification |
| --- | --- | --- | --- |
| 1.32 | 1.32.0 | `kindest/node:v1.32.11` pinned by digest | Required before release |
| 1.34 | 1.34.0 | `kindest/node:v1.34.3` pinned by digest | Required before release |
| 1.37 | 1.37.0 | `kindest/node:v1.37.0` pinned by digest | Required before release |

The exact node-image digests and envtest SHA-512 values are in
[`release/manifest.json`](../../release/manifest.json). Envtest verifies API server
schema/controller behavior; kind additionally verifies Helm lifecycle, RBAC,
managed/BYO runtime behavior, traffic, activation/LKG and recovery. A passed profile
does not certify every Kubernetes distribution, CNI, admission configuration or
future minor. In particular, NetworkPolicy enforcement still depends on the CNI.

The Kubernetes project maintains a moving subset of current release branches.
EgressFox qualification does not extend an upstream Kubernetes minor’s security
support. Consult the [Kubernetes version-support policy](https://kubernetes.io/releases/version-skew-policy/#supported-versions)
when choosing a cluster version.

## Commands

Run the minimum profile locally by default:

```sh
make test-envtest
make helm-check
make e2e-kind
```

Select a qualified profile explicitly:

```sh
K8S_VERSION=1.34 make test-envtest helm-check e2e-kind
```

Run the entire release matrix (Docker or configured Podman plus `kubectl` required
for kind):

```sh
make k8s-compat
```

The normal pull-request gate runs the minimum envtest/Helm profile and one minimum
kind E2E. The full profile matrix runs on scheduled or manually requested trusted CI
and in release validation. Unknown minor versions fail before an unverified binary
or mutable node image can be used.

## Maintaining the contract

To add or remove a Kubernetes minor, update the central manifest with official
controller-tools envtest checksums and a digest-pinned kind image, then update this
table and run the full matrix. Review Kubernetes API deprecations, Helm rendering,
CRD generation and controller-runtime/library compatibility; do not alter module
versions solely to make a historical cluster test pass. Preserve the last successful
qualification evidence in the associated execution plan and release notes.
