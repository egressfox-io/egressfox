# ADR 0017: Leaf ProxyPools and composable policy candidates

Date: 2026-09-23. Status: accepted future architecture.

## Context

The current `ProxyPool` is an inventory assembled from one or more sources. The
current `EgressGateway` refers to one pool, and the future P1 M9 adds target-aware
profiles over a shared inventory before M10 introduces bounded routing policy.
Different policies will need candidate sets from several logical inventories and
profile contexts without duplicating source configuration or making inventory
resources recursive.

## Decision

`ProxyPool` remains a leaf inventory boundary. One pool may combine multiple sources;
source parsing, normalization, admission, canonical endpoint identity,
deduplication and provenance remain inventory responsibilities. A pool does not
include another pool and has no inheritance, recursive references or cycles.

Future candidate composition belongs to selection/policy intent and can combine
one or more `(ProxyPool, Profile)` candidate sets. A profile supplies target/probe/
selection context over a shared pool inventory; it does not require a duplicate pool
per destination. Named candidate groups may then be referenced by bounded EgressPolicy
routing intent for an EgressGateway. Geography is only example metadata; it is not
a built-in pool type or semantic primitive.

When inputs overlap, canonical full endpoint identity is deduplicated before
eligibility/scoring and provenance is unioned. One logical connection discovered
through multiple sources/pools/groups does not become several independent proxies.
Existing credential-revision and provenance semantics remain authoritative.

Do not introduce a separate `ProxyGroup` CRD or another resource solely to express
candidate composition. Prefer bounded policy representation over a mega-CRD that
combines sources, inventory, probes, selection, routing, runtime, outputs and
observability.

## Roadmap constraints

- M8 source refresh changes source state; it must not itself imply a new artifact or
  runtime generation.
- M9 profile identity and scheduling must let one leaf inventory be evaluated under
  multiple target contexts and must not make future Pool/Profile compositions
  impossible.
- M10 must not encode a permanent `one Gateway -> exactly one candidate Pool`
  restriction. It must leave room for bounded named groups composed from multiple
  Pool/Profile inputs. Exact syntax and supported routing remain its design gate.
- M11/M12 report selection, no-op, publication repair and activation outcomes
  without exposing confidential connection/artifact identity.

## Alternatives rejected

- Recursive Pool-of-Pools composition introduces cycles, ownership, precedence and
  failure semantics into inventory reconciliation.
- Duplicating pools for each destination repeats sources and breaks the separation
  between inventory and target-specific evidence.
- A new reusable ProxyGroup API/CRD is not justified before composition semantics
  and consumers establish a need.
- Country-specific resource semantics would overfit examples and conflate mutable
  provider assertions with inventory identity.

## Consequences and unresolved details

The current API remains one Gateway-to-one-Pool until a later designed API changes
it. M9/M10 composition is future, not current capability. Candidate-group schema,
filter language, profile references, validation and empty/direct/block behavior
remain open and are gated by Q16. Multiple-source pool behavior already implemented
in M6 is preserved.
