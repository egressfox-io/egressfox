# M5: Adaptive standalone reconciliation

Status: in progress. Prepared: 2026-09-19. Started: 2026-09-19.
Branch/baseline: `feat/adaptive-reconciliation` from `f587b12`.

## Objective and boundaries

Implement M5's deterministic evidence-to-LKG loop: common eligibility, static and
lowest-latency baselines, an explainable confidence-aware adaptive Top-N selector,
persistent restart-stable decision state, and reusable render/validate/file-publish
reconciliation for both M3 engines. The owning design is
[observations and selection](../designs/observations-and-selection.md); Q5 is resolved
by [ADR 0010](../decisions/0010-deterministic-adaptive-selection.md).

M6, Kubernetes, runtime activation, adaptive probe scheduling, new endpoint
protocols, diversity constraints and a product CLI are outside this plan. Any
untracked user archive remains unrelated and unstaged if present.

## Decisions and entry gates

All strategies share revision/context-specific hard eligibility. Adaptive ranking
uses mean successful duration divided by a 95% Wilson lower success bound. Top-N
retains eligible incumbents with residence and relative hysteresis; observed failure
uses cooldown plus consecutive-success recovery, while confirmed ineligibility can
replace immediately. State is bound to context, policy fingerprint and the M3
protected artifact receipt. SQLite pending checkpoints bridge publication crash
windows without claiming a filesystem/database transaction.

## Checkpoints

- [x] Discover M1–M4 contracts and record Q5, defaults, score and commit protocol.
- [ ] Implement pure selection policy, evidence snapshots, explanations, Top-N and
  deterministic scenario comparison with property/benchmark coverage.
- [ ] Extend protected artifact receipts and SQLite schema for committed/pending
  restart-stable decision checkpoints.
- [ ] Implement reusable standalone plan/apply reconciliation through both renderers,
  native-validation boundary and file publisher, including failure recovery tests.
- [ ] Complete scenarios, architecture/security review, documentation, validation,
  coherent commits and clean handoff without M6 work.

## Progress and evidence

Discovery reviewed the branch/history/tree, AGENTS.md, roadmap, architecture,
endpoint/source, rendering/publication and observation designs, ADRs 0006–0009,
decision queue, threat model, testing/workflow, and the implemented M1–M4 APIs and
tests. Primary references reviewed for Q5 include the NIST Wilson interval guidance,
RFC 8085 latency-estimator guidance, Envoy outlier recovery behavior and Google SRE
load-balancing/overload guidance. No unresolved product-defining M5 gate remains.

## Resume and handoff

Implement `internal/selection` first, then protected receipt/checkpoint persistence
and `internal/reconcile`. Keep the selector free of SQL, engine and Kubernetes imports.
Before completion update this record with exact commands/results, mark M5 in the
roadmap, update the plan/ADR indexes and decision queue, and leave the task branch
clean. Do not start M6.
