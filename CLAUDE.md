# Claude Code instructions for EgressFox

@AGENTS.md

This file defines how Claude should work efficiently in this repository.

`AGENTS.md` and the repository's authoritative docs define product architecture,
Git rules, compatibility contracts, milestone scope, release rules, and engineering policy.

## Working style

Work like a senior implementation engineer, not a general-purpose repository auditor.

For every task:

1. Read the user task carefully.
2. Inspect Git status, current branch, and recent relevant history.
3. Read only the authoritative docs and code directly relevant to the task.
4. Confirm reported bugs or assumptions against the current code before changing anything.
5. Make the smallest coherent change that satisfies the requested behavior.
6. Add focused regression coverage for changed behavior.
7. Update owning documentation only when behavior or a durable decision changed.
8. Review your own diff before committing.
9. Commit completed task-owned work according to `AGENTS.md`.
10. Stop when the requested scope is complete.

Do not perform a repository-wide review unless explicitly requested.

## Context and token efficiency

Minimize unnecessary context consumption.

Prefer:

- `rg`, `git grep`, `git log`, `git show`, and targeted file reads;
- reading relevant functions/types before whole files;
- following references only when they materially affect the task;
- reusing existing abstractions, fixtures, helpers, and test infrastructure;
- inspecting the smallest owning implementation boundary first.

Avoid:

- rereading documents already inspected in the same session;
- dumping large generated files, logs, CRDs, lockfiles, vendor trees, or artifacts into context;
- reading all ADRs when only one or two own the relevant contract;
- broad searches after the relevant implementation boundary is already known;
- long repository summaries before starting implementation;
- repeating architecture already encoded in `AGENTS.md`;
- speculative exploration of future milestones;
- rereading M1–M8 history when working on a later focused task.

If a task names concrete files, findings, functions, tests, or commits, begin there.

When a search result is sufficient to locate the owning implementation, stop broad discovery and inspect that implementation directly.

## Scope discipline

Do not expand a task merely because adjacent cleanup is possible.

Fix:

- the requested feature;
- confirmed bugs directly coupled to it;
- regressions introduced by the change;
- small correctness issues discovered during an explicitly requested focused review.

Do not automatically:

- refactor unrelated packages;
- redesign working architecture;
- introduce abstractions for hypothetical future needs;
- rename APIs for style;
- rewrite existing tests;
- upgrade dependencies or engines;
- change public CRDs;
- start the next milestone.

If an additional issue is real but not necessary for the current task, report it in the handoff instead of expanding scope.

## Architecture discipline

Preserve the central EgressFox invariant:

> EgressFox decides what configuration should exist.  
> Mihomo or sing-box decides how traffic flows through it.

Never implement proxy/data-plane protocols inside EgressFox.

Preserve the Kubernetes-independent Go core.

Do not introduce Kubernetes concepts into endpoint, source, observation, selection,
policy, rendering, or reconciliation core packages.

Use exact engine-profile capability checks.

Never silently downgrade unsupported:

- protocols;
- transports;
- TLS/security modes;
- authentication;
- routing semantics;
- selection semantics.

Shared inventory, observations, selection state, publication state, and runtime state
must remain separate according to their existing ownership boundaries.

Do not change endpoint identity versions or persistent-state formats casually.

Compatibility changes require explicit analysis and owning documentation.

## Bug-fixing method

When given a suspected bug:

1. Locate the exact code path.
2. Confirm the issue against the current implementation.
3. Reproduce or demonstrate it with the smallest focused test when practical.
4. Identify the root cause.
5. Fix the root cause rather than the symptom.
6. Add a regression test that would fail before the fix.
7. Check adjacent invariants that the fix could affect.
8. Stop once the requested problem is solved.

Do not implement speculative fixes for hypotheses you could not confirm.

Do not turn one bug into a general rewrite.

## Reviews

When explicitly asked to review your implementation or surrounding code, keep the review focused on the requested subsystem.

Prioritize concrete findings involving:

- correctness;
- stale-state publication;
- state ownership;
- starvation and fairness;
- concurrency;
- security boundaries;
- credential leakage;
- identity/evidence isolation;
- compatibility;
- last-known-good behavior;
- resource bounds;
- deterministic behavior;
- lifecycle cleanup.

Do not spend time on subjective style comments unless they hide a correctness or maintainability problem.

For each additional finding, determine whether it is:

1. directly coupled and small enough to fix now; or
2. better reported for a separate task.

Do not use "review" as permission for an unrelated whole-repository audit.

## Testing policy

The maintainer normally performs expensive validation.

Unless the task explicitly authorizes it, DO NOT run:

- `make check`
- `make e2e-kind`
- `make k8s-compat`
- `make docker-build`
- `make release-dry-run`
- full Kubernetes envtest suites
- full race suites
- native engine integration suites
- engine rebuilds
- Docker/Podman builds
- long fuzzing
- long benchmarks
- vulnerability scans
- multi-platform builds

Do not invoke equivalent expensive work indirectly through another script or Makefile target.

Do not launch background test processes.

You should still WRITE all focused tests required by the implementation.

By default you may run inexpensive hygiene checks such as:

```sh
gofmt
git diff --check
git diff --cached --check
sh -n <modified-shell-script>
```

Run focused unit tests only when the user/task explicitly permits test execution.

If a supposedly lightweight test starts downloading engines, building containers, starting Kubernetes, or consuming substantial resources, stop and defer it to the maintainer.

Never claim an unexecuted or skipped test passed.

At handoff, give the maintainer exact copy-paste-ready validation commands using actual repository targets and test names.

## Test design

Prefer focused deterministic regression tests.

Use:

- fake clocks instead of sleeps;
- controlled fixtures instead of public services;
- existing synthetic endpoints and engines;
- table-driven tests where they improve clarity;
- actual subsystem boundaries instead of testing only helpers;
- explicit failure cases for stale state and incompatible contexts.

Tests should verify observable behavior and invariants, not private implementation constants.

Do not weaken tests to make a change pass.

Do not remove existing meaningful coverage.

Do not replace meaningful integration assertions with mocks when the owning test layer already has suitable fixtures.

## Documentation

Documentation is part of the implementation when behavior changes.

Update the owning document, not every document mentioning the subsystem.

Prefer:

- an existing ADR for an already-decided contract;
- the current execution plan for implementation/qualification evidence;
- the owning design document for behavioral semantics;
- roadmap changes only when milestone scope/status actually changes.

Do not create duplicate design documents.

Do not rewrite completed milestone history.

Do not mark validation complete when the maintainer has not executed it.

Keep implementation-complete and qualification-complete distinct.

## Git

Follow `AGENTS.md` exactly.

In particular:

- never do substantial work on `main`;
- preserve unrelated user work;
- create or use a descriptive task branch;
- inspect the diff before staging;
- stage only intentional files;
- review the staged diff;
- commit completed work yourself;
- use the repository's emoji Conventional Commit format;
- leave the branch clean and reviewable.

Never:

- push;
- merge;
- tag;
- publish;
- force-push;
- rewrite unrelated history;
- reset away user work;
- modify remote settings;

unless explicitly authorized.

## Decision making

Resolve implementation details autonomously from:

1. current code;
2. tests;
3. owning design docs;
4. ADRs;
5. execution plans.

Ask the user only when a genuinely product-defining decision remains ambiguous after consulting those sources.

Do not ask questions about implementation details you can safely determine from the repository.

If two reasonable implementations exist, prefer the one that:

- changes less code;
- preserves existing contracts;
- reuses existing abstractions;
- keeps work bounded;
- is easiest to regression-test;
- avoids new persistent state;
- avoids new public API;
- avoids future migration burden.

## Code quality

Prefer boring, explicit Go over clever abstractions.

Keep functions and state ownership understandable.

Do not introduce:

- speculative interfaces;
- unnecessary generic helpers;
- duplicated canonical models;
- arbitrary `map[string]any` for connection-critical semantics;
- hidden fallback behavior;
- unbounded goroutines;
- unbounded queues;
- unbounded caches;
- unbounded Kubernetes status;
- silent semantic downgrades.

Errors must be bounded, actionable, and secret-safe.

Never log or expose:

- credentials;
- complete subscription URLs;
- complete endpoint URIs;
- raw generated engine configuration;
- confidential connection revisions;
- protected cache contents;
- secret-bearing headers.

## State ownership

Be explicit about whether state is:

- source-scoped;
- pool-scoped;
- profile-scoped;
- target-scoped;
- engine-profile-scoped;
- Gateway-scoped;
- runtime-scoped.

Do not share state across contexts unless all identity dimensions make reuse valid.

In particular:

- shared inventory may be reused;
- compatible observation evidence may be reused only under the full existing observation identity;
- Gateway anti-flap/selection state stays Gateway-scoped unless an ADR says otherwise;
- publication/activation state must not leak between Gateways;
- target/profile mutation must invalidate stale work where required.

When introducing a cache, cursor, budget, receipt, or scheduler state key, explicitly verify that its key includes every semantic dimension required for safe reuse.

## Scheduling and resource bounds

Preserve bounded work.

For schedulers and reconcilers:

- avoid starvation;
- avoid hot loops;
- avoid repeatedly scanning the same incompatible prefix;
- preserve deterministic progression;
- respect exact engine capabilities before spending bounded probe capacity;
- avoid multiplying work unnecessarily across equivalent Gateways/profiles;
- cancel or invalidate obsolete work after configuration changes;
- keep fair progress between independently evaluated contexts.

Do not solve bounded scheduling by simply making limits extremely large.

## Stale-work protection

For operations that span acquisition, probing, selection, rendering, validation, and publication, verify that the final publication boundary still represents the configuration that was evaluated.

Pay attention to:

- Kubernetes resourceVersion/generation changes;
- source-cache expiry;
- target revision changes;
- profile mutation/removal;
- engine-profile changes;
- pending selection state;
- publication receipts.

Preserve last-known-good behavior when rejecting stale candidates.

Never publish a new generation based on state that became invalid during the operation.

## Engine integration

Mihomo and sing-box are external data-plane engines.

EgressFox may:

- parse supported connection representations;
- normalize connection semantics;
- determine exact engine capability;
- render native configuration;
- invoke native validation;
- execute through-engine probes.

EgressFox must not:

- implement proxy handshakes itself;
- silently convert unsupported transports/security modes;
- bypass capability checks;
- inject arbitrary native config from subscriptions;
- use direct fallback to make tests pass.

Use the exact pinned engine versions and build profiles defined by the repository.

## Kubernetes

Keep Kubernetes-specific logic at adapter/controller boundaries.

Preserve:

- namespace-scoped ownership;
- Secret handling;
- SnapshotGuard semantics;
- immutable generation activation;
- authenticated managed Gateway service;
- LKG behavior;
- bounded status;
- deterministic reconciliation.

Do not introduce cross-namespace references, HA architecture, or additional controllers unless explicitly required by the current milestone.

## Security

Treat network authorization, source acquisition, endpoint probing, and target probing as separate security boundaries.

Preserve:

- SSRF protections;
- metadata/special-use address restrictions;
- DNS resolution validation;
- IPv4/IPv6 normalization;
- UDP/QUIC authorization;
- TLS SNI semantics;
- secret redaction;
- native process isolation.

Do not relax security rules to make a fixture or E2E scenario pass.

Fix the fixture if the fixture is wrong.

## Performance

Do not optimize speculatively.

When working on scheduling, reconciliation, inventory, or persistence:

- preserve existing complexity where acceptable;
- avoid accidental O(N²) expansion in new hot paths;
- avoid copying full inventories per profile or Gateway;
- avoid N × P work unless explicitly bounded;
- reuse existing shared state where semantics allow.

If a performance concern is real but outside task scope, report it.

## Public API

Do not change CRDs or public behavior casually.

Before changing public API verify:

- backward compatibility;
- defaults;
- stored-object behavior;
- generated CRDs;
- Helm copies;
- examples;
- API compatibility tests;
- migration implications.

Prefer internal fixes when public API changes are not required.

## Completion criteria

Before declaring implementation complete, verify from the diff that:

- the requested behavior is implemented;
- confirmed root causes are fixed;
- focused regression tests exist;
- compatibility boundaries are preserved;
- security boundaries remain intact;
- documentation matches actual behavior;
- no unrelated functionality was added;
- no expensive validation is falsely claimed;
- task-owned changes are committed;
- the working tree is clean, or unrelated pre-existing changes are clearly reported.

Then stop.

Do not continue polishing once the task is complete.

## Handoff format

Keep the final response concise.

Use this structure:

### Result

What changed.

### Findings

Confirmed root causes and any additional concrete issue found during the requested focused review.

### Tests

Tests added or modified.

Clearly distinguish:

- executed;
- written but not executed;
- skipped/deferred.

### Maintainer validation

Exact copy-paste-ready commands to run, in recommended order.

### Git

Branch, commits, and final working-tree status.

### Remaining

Only genuine blockers or deferred issues.

Do not repeat a long project overview in the handoff.
