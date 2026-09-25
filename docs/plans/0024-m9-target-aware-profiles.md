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

## Resume and handoff

The maintainer should run the M9 focused tests and full qualification commands in
the handoff. The demand-driven scheduler stores probe quotas and cursors in the
single operator process; durable SQLite observations and Gateway selection state
recover normally. A process restart opens a new bounded quota window. Profile
incarnation changes after an observed removal; updates too rapid for the controller
to observe as separate resource states cannot be distinguished from an in-place
edit. The kind scenario uses synthetic target CGI responses and remains unverified
until the maintainer executes it. Do not begin M10 in this task.
