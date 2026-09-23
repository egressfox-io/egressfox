# M8 resilient HTTP sources

Status: in progress. Date: 2026-09-23. Branch: `feat/resilient-http-sources`.
Baseline: `055c5cd`.

## Objective and boundaries

Complete [P1 M8](../roadmap/p1.md#m8--resilient-http-source-refresh): managed
HTTP refresh, protected bounded source cache, conditional requests, fallback,
restart recovery and Kubernetes integration. Preserve Secret sources and M7
activation. M9 profiles and later work are outside this plan.

## Decisions and entry gates

Q14 is resolved by [ADR 0018](../decisions/0018-resilient-http-sources.md).
The repository had a clean `main` at baseline; no pre-existing changes were present.

## Checkpoints

- [x] API, conditional acquisition and cache storage with tests.
- [x] Bounded refresh scheduling, operator integration, status and regression tests.
- [ ] Envtest/kind evidence, documentation, full validation and clean commits.

## Progress and evidence

Repository discovery confirmed M7 is implemented and M8 is unimplemented. The
operator already has a protected SQLite PVC, Secret indexes and artifact equality;
M8 builds on those paths.

- `8f9d91a` records Q14 in ADR 0018 before code.
- `ceaa8e6` adds typed conditional HTTP results, bounded retry and protected
  SQLite schema v3 cache with reopening, integrity and migration tests.
- The operator checkpoint adds the validated Secret/HTTP CRD union, private
  compatibility identity, cached inventory reconstruction, four-worker leader
  refresh, targeted Pool/Gateway events and stale-input guards. Focused Go tests,
  race tests and pinned 1.32 envtest passed; kind and final validation remain.

## Resume and handoff

Implement the ADR in source, state and operator layers, then validate each checkpoint.
