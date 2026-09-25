# M9 target-aware profiles

Status: in progress. Date: 2026-09-25. Branch: `codex/m9-target-aware-profiles`.
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

- [ ] Named API, generated CRDs, validation, documentation and compatibility tests.
- [ ] Profile-specific evidence, bounded scheduling, selection and publication tests.
- [ ] Status, envtest and controlled parallel kind scenario.
- [ ] Lightweight verification, reviewed commits and clean handoff.

## Progress and evidence

Branch created from a clean `e1a6469` checkout. No expensive validation is to be
run in this task.

## Resume and handoff

Implement the API and adapter against the existing M4/M5 packages. Record exact
lightweight checks and unfinished qualification here.
