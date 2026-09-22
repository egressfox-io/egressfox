# M7 managed single-replica gateway and exact-generation activation

Status: complete.
Date: 2026-09-22. Branch: `codex/m7-managed-gateway`. Baseline: `e8ca4d6`.

## Objective and boundaries

Implement [P1 M7](../roadmap/p1.md#m7--managed-single-replica-gateway-and-activation):
an explicit managed `EgressGateway` mode that produces an owned authenticated SOCKS
ClusterIP Service and exact-generation activation evidence for both pinned engines.
Existing omitted-runtime objects remain BYO. This plan excludes every M8+ feature,
runtime replicas, live reload, arbitrary Pod settings, new protocols and workload
attachment.

## Decisions and entry gates

[ADR 0013](../decisions/0013-managed-gateway-activation.md) resolves Q7/Q13 before
CRD generation. Evidence is the exact release-manifest source commits for Mihomo
and sing-box, Kubernetes Deployment/Secret behavior, the existing renderer/native
validator tests, and ADRs 0008/0011/0012. The release-controlled combined image is
the M7 runtime image; managed auth is generated, Secret-backed and stable for the
Gateway lifetime. Deletion is repaired from protected generation data; coordinated
rotation is deferred.

## Checkpoints

- [x] Resolve Q7/Q13, research both exact engines, and freeze API/ownership,
  listener, generation, rollout, activation and deletion contracts.
- [x] Add core authenticated listener semantics and exact-profile renderer/native
  tests without changing BYO output.
- [x] Add managed API, generation/auth publication, desired object planning,
  ownership checks, activation observation, cleanup and controller wiring.
- [x] Extend CRDs, RBAC, Helm/image health helper, samples, unit/envtest/kind tests
  and release coverage.
- [x] Complete security/architecture/operations documentation, full validation,
  architectural review, atomic commits and clean handoff.

## Progress and evidence

- Repository discovery found a clean `main` at `e8ca4d6`, four local P1-design
  commits ahead of `origin/main`; no user changes were present.
- Exact pinned sources confirm username/password SOCKS support in Mihomo 1.19.31
  and sing-box 1.14.1. The current image already contains both source-built
  derivatives and their Q12 notice/source/provenance contract.
- Kubernetes rolling update and immutable Secret documentation supports the chosen
  one-replica surge/LKG model. Readiness needs an authenticated loopback handshake;
  a TCP-open probe alone is insufficient.
- Core/renderers now express the common managed listener without Kubernetes types,
  and both exact profiles natively validate username/password SOCKS. Existing BYO
  renderer goldens remain byte-identical.
- The alpha API admits only omitted-runtime BYO or explicit `managed: {}`. The
  adapter owns immutable auth/generation Secrets, a secure Deployment, stable
  ClusterIP Service and ingress-only NetworkPolicy, and observes exact-generation
  activation separately from overall LKG readiness.
- Security review found that regenerating credentials after client-auth Secret
  deletion could expose credentials before the old LKG accepted them. M7 therefore
  repairs the same credentials from protected generation data and explicitly
  defers rotation rather than claiming an unsafe one-step protocol.
- Envtest exercised managed publication, admitted immutable Secrets and controller
  ownership through a real API server, proved idempotent Deployment reconciliation,
  and verified managed cleanup. The kind test exercised BYO regression, both exact
  managed engines, mandatory SOCKS authentication and traffic, failed-rollout LKG,
  auth/Service repair, bounded generations, operator restart, and both mode
  transitions before Helm upgrade/uninstall.
- The final release dry run built reproducible operator binaries and the combined
  managed runtime image for `linux/amd64` and `linux/arm64`, produced SPDX JSON
  SBOMs for binaries/engines/image, ran Go/engine/image vulnerability gates,
  packaged Helm, built redistributable engine source archives, and verified every
  release file through `SHA256SUMS`. It used no skip flags and published nothing.
  A clean-cache sing-box `go mod tidy` fetched upstream cronet modules for many
  non-release platforms and was slow; narrowing that fetch is a tooling optimization,
  not an M7 correctness or release-matrix blocker.

Validation results:

- `GOCACHE=/tmp/egressfox-go-cache make fmt generate manifests helm-check` — passed.
- `GOCACHE=/tmp/egressfox-go-cache make test-envtest` — passed against pinned
  Kubernetes 1.37.0 envtest assets.
- `make e2e-kind` — passed on kind v0.33.0 / Kubernetes 1.37.0 with the complete
  both-engine, failure/recovery and mode-transition matrix; the isolated cluster
  was deleted by the test trap.
- `GOCACHE=/tmp/egressfox-go-cache make check` — passed generated drift, formatting,
  vet, all race tests, build, offline docs, Helm lint/render, release-manifest
  validation and whitespace checks.
- `GOCACHE=/tmp/egressfox-go-cache make vuln` — passed with zero reachable
  vulnerabilities; govulncheck reported one imported-package vulnerability that
  the EgressFox call graph does not reach.
- `GOCACHE=/tmp/egressfox-go-cache VERSION=v0.2.0-alpha.1 make release-dry-run` —
  passed without `RELEASE_SKIP_IMAGE` or `RELEASE_SKIP_VULN`; both platforms,
  SBOMs, scans, Helm package, source/license artifacts and checksums completed with
  no registry or release publication.
- `git diff --check` — passed.

Architecture review confirms that managed mode remains an explicit opt-in and the
engine remains the data plane. Modified upstream bytes fail the existing manifest
verification; untrusted PR CI has no release credentials. Status does not equate
publication with activation, and managed readiness is deliberately bounded to the
authenticated local listener rather than traffic health. Helm/API/image metadata
share the existing release version flow. M7 adds no source cache, richer routing,
new protocols, generic output, cross-namespace access, runtime HA or workload
injection.

## Resume and handoff

M7 is complete on `codex/m7-managed-gateway`. The next bounded milestone is P1 M8,
resilient HTTP source refresh and durable cache. It must start from the authoritative
roadmap and resolve Q14 before changing source behavior; no M8 work belongs in this
branch.
