# M9 target-aware profiles

Status: implemented; the pre-hardening implementation passed the maintainer's existing suite; final scheduler-hardening requalification pending. Date: 2026-09-25. Branch: `codex/m9-target-aware-profiles`.
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
- [x] Maintainer focused Go tests, `make check`, 1.32 envtest, API compatibility,
  vulnerability scan and controlled profile kind traffic.
- [x] Post-implementation review fixes (2026-09-30): named-profile evidence
  accumulation, probe-context-scoped budget/cursor/working set, failed-profile
  Gateway status and documentation reconciliation. Tests prepared; maintainer
  re-validation pending.
- [x] Maintainer reported the existing test suite passing on the pre-hardening
  implementation (2026-10-01).
- [x] Final scheduler hardening (2026-10-01): one scheduler for default and named
  profiles, M5-fed maintained cohort with pinned selections, evidence cadence
  independent of the source refresh interval, `topN` capped at the maintainable
  cohort. Focused tests written; not executed.
- [ ] Maintainer requalification of the final scheduler hardening, including the
  full kind suite and remaining native/release checks.

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
required a maintainer rerun before qualification.

On 2026-09-28 the maintainer's focused `profiles` kind run passed in 106 seconds
(`dist/e2e-logs/20260928-172349-33110`). Its log reached Ready for the alpha,
beta and default Gateways, and the script completed its config, authenticated
SOCKS traffic and unrelated-profile generation assertions. This confirms the
controlled profile scenario after the IPv4 fixture change. Only the `profiles`
scenario appears in that log directory; the full kind suite and native checks
remain unqualified.

On 2026-10-01 the maintainer reported that the existing test suite passed on the
pre-hardening implementation. A follow-up review then confirmed four scheduler
defects: one failed observation evicted a working-set member ahead of M5's failure
streak; an explored endpoint that became selected while the eight-member working
set was full was never re-probed and went stale; Gateway requeue followed the
source refresh interval plus up to 10% jitter, so a 14-minute refresh could not
keep three samples in the 30-minute evidence window and a 24-hour refresh left the
default profile unable to qualify; and the default profile kept a per-Gateway
cursor while sharing the context budget. The final hardening replaces both
schedulers with the single contract in
[ADR 0024](../decisions/0024-target-aware-profiles.md). Its focused tests are in
`internal/operator/profile_scheduling_test.go` and
`internal/controller/scheduling_internal_test.go`; they were written but not run.

## Resume and handoff

The maintainer should requalify the final scheduler hardening, including the full
kind suite and remaining native/release checks, before marking M9 fully qualified.
The demand-driven scheduler stores round windows, cursors, cohorts and Gateway
selection pins per probe context in the single operator process; durable SQLite
observations and Gateway selection state recover normally. A process restart
begins a new round immediately. Profile incarnation changes after an observed
removal; updates too rapid for the controller to observe as separate resource
states cannot be distinguished from an in-place edit. The kind scenario uses
synthetic target CGI responses. Do not begin M10 in this task.
