# M9 target-aware profiles

Status: implementation prepared; maintainer qualification pending. Date: 2026-09-25. Branch: `codex/m9-target-aware-profiles`.
Baseline: `e1a6469`.

## Objective and boundaries

Implement [M9](../roadmap/p1.md#m9--target-aware-selection-profiles) over one
shared source inventory. Keep M10 routing and expensive validation outside this
task. The maintainer will execute native, envtest, kind, race and full checks.

## Decisions and entry gates

Q15 is resolved for the public API by [ADR 0024](../decisions/0024-target-aware-profiles.md).
Existing ADRs 0009, 0010, 0013, 0018 and 0021 continue to govern probes,
selection, activation, source cache and engine capability.

## Checkpoints

- [x] Named API, generated CRDs, validation, documentation and compatibility tests.
- [x] Profile-specific evidence, bounded scheduling, selection and publication guard tests.
- [x] Status, envtest assertions and controlled parallel kind scenario prepared.
- [x] Lightweight verification, reviewed commits and clean handoff.
- [ ] Maintainer full checks, envtest, kind traffic and vulnerability qualification.

## Progress and evidence

Branch created from a clean `e1a6469` checkout. Q15 was committed as `81e305b`.
Focused Go profile/selection/reconcile/operator/controller tests, a repository-wide
compile-only `go test ./... -run '^$'`, `make generate-check`, `make docs`,
`git diff --check`, `sh -n` and the E2E configuration test passed.
The full envtest suite, kind, native traffic, race suite, Docker and vulnerability
scan were intentionally not run under the maintainer's instruction.

Maintainer qualification on 2026-09-25 passed focused Go tests, `make check`,
Kubernetes 1.32 envtest, the 1.32/1.34/1.37 API compatibility matrix and
`make vuln` (zero reachable vulnerabilities). Both focused and full kind runs
stopped in `profiles` immediately after Helm install: the shared fixture read
`operator_deployment` before the new scenario initialized it. The scenario now
sets that deployment name and cleans up its generated certificate directory,
matching the existing scenarios. Controlled profile traffic remains unqualified
until the maintainer reruns kind.

The next focused kind run reached the managed Gateways: the pool admitted two
endpoints and the default Gateway activated, while both named Gateways remained
unready after five minutes. The saved diagnostics omitted Gateway Conditions and
probe outcomes, so the cause is not proven. The controlled CGI target compared
`REMOTE_ADDR` to IPv4 Pod addresses while BusyBox `httpd -p 8080` may accept
IPv4 through an IPv6 listener. The fixture now binds explicitly to IPv4 and
the kind runner captures resource Conditions on failure. Named profile traffic
still requires a maintainer rerun before qualification.

## Resume and handoff

The maintainer should rerun the focused and full kind scenarios after this fixture
fix. The demand-driven scheduler stores probe quotas and cursors in the
single operator process; durable SQLite observations and Gateway selection state
recover normally. A process restart opens a new bounded quota window. Profile
incarnation changes after an observed removal; updates too rapid for the controller
to observe as separate resource states cannot be distinguished from an in-place
edit. The kind scenario uses synthetic target CGI responses; traffic assertions
remain unverified until a rerun reaches them. Do not begin M10 in this task.
