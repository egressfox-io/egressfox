# M8 resilient HTTP sources

Status: complete. Date: 2026-09-24. Branch: `feat/resilient-http-sources`.
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
- [x] Envtest/kind evidence, documentation, full validation and clean commits.

## Progress and evidence

Repository discovery confirmed M7 is implemented and M8 is unimplemented. The
operator already has a protected SQLite PVC, Secret indexes and artifact equality;
M8 builds on those paths.

- `8f9d91a` records Q14 in ADR 0018 before code.
- `ceaa8e6` adds typed conditional HTTP results, bounded retry and protected
  SQLite schema v3 cache with reopening, integrity and migration tests.
- `36a4574` adds the validated Secret/HTTP CRD union, private compatibility
  identity, cached inventory reconstruction, four-worker leader refresh, targeted
  Pool/Gateway events and stale-input guards.
- `ebd18a6` adds direct v2-to-v3 migration and unchanged-body/new-validator tests,
  plus a controlled kind provider. The first kind attempt exposed BusyBox `httpd`
  withholding conditional headers from CGI; the provider now reads raw requests
  through BusyBox `nc`. The full rerun passed.

## Validation and exit evidence

- `GOCACHE=/tmp/egressfox-m8-go-cache make generate manifests`, `make fmt`,
  `make check`, `make test-envtest`, `make helm-check`, `make release-validate`,
  `make docs`, and `git diff --check` passed. The full race suite and focused
  source/state/operator/controller race tests passed.
- `make vuln` completed against the pinned online scanner: zero called
  vulnerabilities; one imported package finding was not called by the module.
- `make e2e-kind` passed on the pinned Kubernetes 1.32 profile. It covered the
  existing BYO and managed regression paths, both engine profiles, authenticated
  controlled traffic, conditional 304 with stable generations, outage fallback,
  restart recovery, changed content, malformed response preservation and cache
  expiry. The successful run removed its temporary cluster.
- Kubernetes 1.34 and 1.37 kind profiles were not run in this task; the roadmap's
  release matrix still requires their separate qualification before a release
  claim. No remote publication was attempted.

## Security and compatibility review

Existing Secret sources remain valid and can mix with managed HTTP sources. URL
and optional Authorization bytes stay in same-namespace Secrets. Every HTTP attempt
uses the M2 dial/redirect/size bounds; plain HTTP, private destinations and insecure
source TLS require separate opt-ins. Cache bodies and validators remain on the
private SQLite PVC, with integrity checks and bounded retention. URL/auth, format,
admission and acquisition-policy changes invalidate compatibility, while Secret
metadata alone does not. Cache and Secret versions are checked before Gateway
publication so obsolete work cannot replace newer desired state. Equivalent final
artifacts keep the same managed generation.

## Handoff

M8 is complete; [M9 target-aware selection profiles](../roadmap/p1.md#m9--target-aware-selection-profiles)
is the next bounded milestone. It can consume the refreshed shared inventory
without duplicating HTTP acquisition. Do not treat M9 as implemented by this plan.
