# M7 managed single-replica gateway and exact-generation activation

Status: in progress.
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
the M7 runtime image; managed auth is generated, Secret-backed and deletion-rotated.

## Checkpoints

- [x] Resolve Q7/Q13, research both exact engines, and freeze API/ownership,
  listener, generation, rollout, activation and deletion contracts.
- [ ] Add core authenticated listener semantics and exact-profile renderer/native
  tests without changing BYO output.
- [ ] Add managed API, generation/auth publication, desired object planning,
  ownership checks, activation observation, cleanup and controller wiring.
- [ ] Extend CRDs, RBAC, Helm/image health helper, samples, unit/envtest/kind tests
  and release coverage.
- [ ] Complete security/architecture/operations documentation, full validation,
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

## Resume and handoff

Next implement the authenticated listener domain and renderer goldens, then the
managed Kubernetes adapter. Key risks are preserving publication checkpoint
semantics across two target forms, no-op convergence, exact-owned cleanup, rollout
status attribution, and deterministic real-cluster failed-rollout evidence.
