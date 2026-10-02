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
- [x] Scheduler follow-up (2026-10-01, after review of `bb0777b`): shared-capacity
  reservation with constrained re-planning, exploration-first rounds with a
  bounded exploration time and maintenance last, released failed rounds, and
  `topN`/overload `SelectionReady` reasons. Focused tests written; not executed.
- [x] Reservation lifecycle fixes (2026-10-01, after review of `e8553d6`): demand
  for M5-rejected endpoints no longer reserves capacity, and writes without a
  confirmed receipt and committed checkpoint are held as uncertain demand until
  the actual receipt is read. Focused tests written; not executed.
- [x] Uncertain-publication maintenance (2026-10-01, after review of `bd7a0ab`):
  reservations fit the current and attempted outputs together before any write,
  held writes never displace the current output, and the actual current output
  (BYO Secret or managed status generation) is read before the next round.
  Focused tests written; not executed.
- [x] Final pre-qualification pass (2026-10-01, after review of `fc71726`): the
  missing `checkOutputMaintained` test helper is defined over completed probes,
  and an evicted M5-rejected endpoint leaves every Gateway's demand. Static
  review only; nothing compiled or executed.
- [x] Qualification fixes (2026-10-02): the probe round window is bounded by a
  shorter pool refresh interval, so the 30-second kind fixtures retry a first
  round that found no usable evidence; and a managed reconciliation that
  publishes nothing keeps the Deployment's current target instead of a stale
  cached generation, which had rolled quota-blocked rollouts back on Kubernetes
  1.34. Failed kind scenarios now retain rollout conditions, events and runtime
  logs.
- [ ] Maintainer requalification of the final scheduler hardening at the final
  branch HEAD, including the full kind suite and remaining native/release
  checks. PASS results from any earlier commit (including `bb0777b`, `e8553d6`,
  `bd7a0ab` and `fc71726`) do not qualify later code.

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

A review of `bb0777b` confirmed three further gaps. Each Gateway's `topN` was
capped separately while the cohort is shared, so two Gateways' selections could
exceed it and a published endpoint could silently miss admission. A round ran
maintenance first and evaluated after the whole batch, so slow exploration (up to
six two-minute timeouts on one named context) could stale healthy maintenance
evidence before evaluation. And M5 received the capped `topN`, so
`Decision.Degraded` could not report the cap. The follow-up reserves each
non-empty decision against the other Gateways' maintained demand before
rendering, re-plans among maintained endpoints when it does not fit, commits
demand only after publication, orders rounds exploration first with a
time-bounded exploration phase and demanded members last, reports overload, and
keeps the requested `topN` for status reasons. Its tests are in
`internal/operator/profile_scheduling_test.go`,
`internal/controller/reconcile_test.go` and `internal/reconcile/reconcile_test.go`.

A review of `e8553d6` confirmed two lifecycle defects. Published demand stayed
protected after M5 rejected its endpoints, so two Gateways holding the same full
failed cohort refused each other's replacements indefinitely while both kept
reconciling. And any error after the external write restored the previous demand,
so an output that already carried the new endpoints could lose their
maintenance. `reconcile.Result` now reports the external publication state
(none, uncertain, written, confirmed) separately from the error and from the
durable checkpoint commit; the operator holds such writes as uncertain demand and
resolves them from the actual receipt. Publisher create/update errors are marked
uncertain.

A review of `bd7a0ab` confirmed that an uncertain hold in a full cohort evicted
the Gateway's previous-only endpoints even though the actual output (a lost BYO
write, or a managed generation that never became current) could still carry
them, and that receipt resolution ran after the round and never re-admitted
them. Reservations now count the Gateway's own current output, so a hold always
fits without displacing it; infeasible transitions are refused before writing.
Resolution reads the BYO Secret or the managed status generation before the
round.

## Resume and handoff

The maintainer should requalify the final scheduler hardening, including the full
kind suite and remaining native/release checks, before marking M9 fully qualified.
Publication of `v0.1.0-dev.2` additionally needs a fresh GitHub Release and GHCR
preflight; earlier read-only checks are not evidence for the release run.
The demand-driven scheduler stores round windows, cursors, cohorts and Gateway
demand per probe context in the single operator process; durable SQLite
observations and Gateway selection state recover normally. A process restart
begins a new round immediately with empty cohorts and demand, so an incumbent
not reached by the first round after an outage longer than `Freshness` can be
replaced once. Gateway reconciliations run serially in the controller; a
second concurrent caller would read the previous round's evidence while a
round is in flight. A Gateway whose healthy output fills its profile's cohort
cannot move to an endpoint outside it until a protected endpoint is released. Profile incarnation changes after an observed
removal; updates too rapid for the controller to observe as separate resource
states cannot be distinguished from an in-place edit. The kind scenario uses
synthetic target CGI responses. Do not begin M10 in this task.
