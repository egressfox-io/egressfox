# ADR 0005: Validated publication with retained last-known-good output

Date: 2026-09-18. Status: accepted.

## Context

Generated configuration includes credentials and can interrupt production
connectivity. Parseable output can still have invalid references, unsupported
semantics, unsafe defaults, or a mismatched engine version.

## Decision

Publish only artifacts that have passed common-model, capability, and target-engine
validation bound to the exact bytes/profile. Compare revisions and content before
writing; make retries idempotent and target updates recoverable. Invalid new
configuration must never replace last-known-good output. Preserve a bounded,
protected previous artifact and publication receipt.

Keep publication success distinct from activation/traffic success. BYO publication
does not imply runtime acknowledgment. Engine/version incompatibilities and empty
or infeasible policy outcomes must fail explicitly rather than silently routing
directly or dropping requested rules.

## Alternatives

Writing while rendering exposes partial data. Syntax-only validation misses engine
semantics. Overwriting and hoping to recover after reload loses the safe baseline.
A universal cross-target transaction is unavailable across files, Kubernetes, and
future remote outputs.

## Consequences

Each target needs ownership, stale-work checks, interruption recovery, and failure
tests. Sensitive LKG backups require access controls and retention. Exact journal,
Secret generation, reload, and rollback mechanisms remain design gates in
[policy and publication](../designs/policy-rendering-publication.md); this ADR does
not claim them implemented.
