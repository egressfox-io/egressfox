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
  selector. Exact engine capability filtering precedes the bounded round-robin
  batch. A pool allows 64 default-profile jobs and eight jobs for each of at
  most eight named profiles, per exact engine and refresh window: at most 256
  admissions across both engines. The operator shares four probe slots across
  Gateway reconciliations. No profile starts a separate source refresher.
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
  its selected profile alongside existing evidence and publication counts. Neither
  exposes endpoint IDs, observations, credentials, target URLs or revisions.

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
