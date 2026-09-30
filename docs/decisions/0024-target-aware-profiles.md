# ADR 0024: Target-aware profiles over a shared pool inventory

Date: 2026-09-25. Status: Accepted for M9 implementation.

## Context

Q15 gates the named profile API. A ProxyPool currently has one required `probe`
and one optional `selection`; a Gateway binds one pool. Endpoint identity, target
revision, exact engine profile and receipt-bound selection state already exist.

## Decision

- A ProxyPool owns at most eight additional named profiles. The existing top-level
  fields remain the reserved `default` profile, without conversion or changed
  defaults. Named entries each contain a complete probe and selection definition;
  fields never inherit from the default. Names are unique DNS labels; `default` is
  reserved. A Gateway's optional `profileRef` selects one additional profile;
  omission or `default` selects the legacy fields. References stay in the pool's
  namespace because a Gateway names only its local pool.
- Subscription acquisition and normalized inventory belong to the pool and are
  shared. A Gateway evaluates only its chosen profile using the M4/M5 probe and
  selector. Exact engine capability filtering precedes scheduling; incompatible
  endpoints never consume probe capacity. Probe state (window budget, exploration
  cursor and working set) belongs to the probe context: pool, exact engine profile,
  target ID (which encodes profile name and incarnation) and target revision.
  Gateways are not part of it, so equivalent Gateways share one budget and cursor,
  and a changed target or recreated profile starts fresh. The default profile
  keeps its 64-job round robin per window. A named context qualifies a bounded
  working set of at most eight endpoints that answered its target: they are
  re-probed every window, and newly explored endpoints are probed MinSamples times
  at once so M5 evidence (unchanged) is reachable on large inventories, while a
  small deterministic cursor explores the rest and replaces failed members. When
  the refresh interval is too long for successive windows to fit MinSamples into
  the evidence window, the working set is also probed in a burst. The budget
  window is the refresh interval capped at the M5 freshness period. A context
  admits at most 30 named jobs per window; with eight profiles, both engines and
  the default profile the pool ceiling is 608 jobs per window. The operator shares
  four probe slots across Gateway reconciliations. No profile starts a separate
  source refresher. Probe state is in-memory, expires after 24 hours unused and
  restarts empty; durable observations and Gateway selection state are unaffected.
- Target IDs include pool identity and an opaque digest of profile name and
  its first observed pool generation. Bounded status retains that incarnation
  while a profile is present and drops it on observed removal. The target revision covers
  canonical URL and effective probe semantics; connection revision and exact engine
  profile remain independent key dimensions. Profile names are not endpoint IDs.
  Selection state remains Gateway scoped and resets when its context or policy
  fingerprint changes. This preserves legacy target IDs and old evidence.
- Missing profiles and invalid target Secrets fail closed. The existing snapshot
  guard checks pool, Gateway, Secret and selected HTTP cache revisions immediately
  before publication. A profile or target mutation therefore cannot promote a
  stale selection. An unrelated profile update may cause a retry, but equal
  validated bytes keep the same generation. Existing published/active LKG remains.
- Pool status reports bounded named-profile incarnations; Gateway status names
  its selected profile and reports eligible/selected counts for that profile's
  current desired selection, which are zero when the profile cannot be resolved or
  evaluated (active/published generations keep the LKG). Neither exposes endpoint
  IDs, observations, credentials, target URLs or revisions.

## Alternatives

Separate pools or source fetches per profile duplicate inventory and cache state.
Putting profile names in endpoint IDs breaks ef1/ef2/ef3 compatibility. Silently
replacing the default profile would invalidate stored objects. A generic policy or
distributed scheduler belongs outside M9.

## Consequences

Profile evaluation is demand-driven by referencing Gateways. The implementation
must bound aggregate demand and lifecycle cleanup as it is completed; the execution
plan tracks those gates. M10 may compose profile outputs without changing source
inventory ownership.
