# M5: Adaptive standalone reconciliation

Status: complete. Prepared: 2026-09-19. Started: 2026-09-19. Completed: 2026-09-19.
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
- [x] Implement pure selection policy, evidence snapshots, explanations, Top-N and
  deterministic scenario comparison with property/benchmark coverage.
- [x] Extend protected artifact receipts and SQLite schema for committed/pending
  restart-stable decision checkpoints.
- [x] Implement reusable standalone plan/apply reconciliation through both renderers,
  native-validation boundary and file publisher, including failure recovery tests.
- [x] Complete scenarios, architecture/security review, documentation, validation,
  coherent commits and clean handoff without M6 work.

## Progress and evidence

Discovery reviewed the branch/history/tree, AGENTS.md, roadmap, architecture,
endpoint/source, rendering/publication and observation designs, ADRs 0006–0009,
decision queue, threat model, testing/workflow, and the implemented M1–M4 APIs and
tests. Primary references reviewed for Q5 include the NIST Wilson interval guidance,
RFC 8085 latency-estimator guidance, Envoy outlier recovery behavior and Google SRE
load-balancing/overload guidance. No unresolved product-defining M5 gate remains.

The selection checkpoint implements a common hard gate, deterministic static and
lowest-latency baselines, Wilson-adjusted adaptive cost, exact relative hysteresis,
residence, failure cooldown/recovery and emergency replacement. Decisions contain
safe structured evidence/reasons and bounded next state. Replay compares all three
strategies on the same frames. Permutation, credential/context isolation, Top-N
shortfall, complete outage, jitter, recovery and redaction tests protect the domain.

Artifact receipts now have a protected value type, and the file publisher can read
the exact owned LKG receipt after its existing recovery checks. SQLite schema v2
migrates schema v1 and stores committed/pending decision checkpoints. Tests prove
restart promotion after publication, discard before publication and refusal to use
state for a mismatched LKG.

`internal/reconcile` composes bounded history summaries, pure selection, M3 policy,
both existing renderers, required checker validation and file publication. It stages
before publish and promotes after receipt readback. Integration tests prove first
publication, byte-identical no-op, restart continuity, obsolete-time rejection,
validation failure, publication failure and zero-eligible LKG retention.

Final validation on Darwin arm64 / Apple M4 Pro:

- `make fmt` — passed.
- `make check` — passed outside the restricted sandbox, including vet, all
  race-enabled tests, build, documentation and whitespace checks. The first sandboxed
  run reached the existing M2 loopback fixture and failed only because bind was denied.
- `make docs` — passed; offline links and anchors are valid.
- `make vuln` — passed with pinned `govulncheck` v1.8.0; no vulnerabilities found.
- `go test -race -count=5 ./internal/selection ./internal/reconcile ./internal/state`
  — passed.
- `go test -run '^$' -fuzz '^FuzzSelectionPermutation$' -fuzztime=10s
  ./internal/selection` — passed 2,631,469 executions without a failure.
- `go test -run '^$' -bench BenchmarkSelectTopN -benchtime=3x
  ./internal/selection` — 15,156,222 ns/op at 1,000 candidates and 198,986,083
  ns/op at 10,000 candidates for Top-10 (development-host evidence only).
- `git diff --check` — passed. No fuzz corpus, engine binary, generated real
  configuration, credential, URI, archive or temporary publication file was added.

## Resume and handoff

M5 is complete on `feat/adaptive-reconciliation`. M6 is next and remains gated by
Q8/Q9 and the Secret portion of Q7. It can adapt the shared reconciliation use case
to Kubernetes desired state and protected Secret output without changing selection,
renderer or artifact semantics. No M6 code, CRD, controller, Secret publisher,
daemon or product CLI was implemented.
