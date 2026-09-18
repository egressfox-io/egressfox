# ADR 0001: Control plane with a Kubernetes-independent core

Date: 2026-09-18. Status: accepted.

## Context

Changing external gateways require repeated assessment and desired configuration.
Mihomo and sing-box already implement protocols and runtime packet/connection
handling. Standalone users need the same selection behavior as Kubernetes users.

## Decision

EgressFox owns acquisition, observations/history, policy evaluation, selection,
rendering, validation, and desired configuration publication. Engines own proxying,
tunneling, runtime routing, balancing, and packet handling. Probe execution may
use an engine through an adapter; it must not grow a proxy stack.

Keep core endpoint, policy, and selection logic independent of Kubernetes types,
clients, and lifecycle. Standalone and operator integrations compose common use
cases with explicit adapters. Engine-specific semantics remain behind renderers
and execution/validation adapters. Unsupported semantics fail explicitly.

## Alternatives

Embedding a new proxy implementation duplicates protocol/security maintenance.
A Kubernetes-only operator would make standalone operation and deterministic
experiments unnecessarily difficult. Delegating all selection to engine URL tests
does not address shared provenance, historical adaptive decisions, or a common
desired-state policy.

## Consequences

Engine compatibility and process/API integration remain real engineering work.
EgressFox can explain its desired selection but cannot claim to control each
connection or observe activation in all BYO deployments. Dependency boundaries
must be enforced in review/tests as code appears. See [architecture](../architecture.md).
