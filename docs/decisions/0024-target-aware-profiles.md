# ADR 0024: Target-aware profiles over a shared pool inventory

Date: 2026-09-25. Status: Accepted for M9 implementation; scheduling amended
2026-10-01 by the final M9 scheduler hardening.

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
  endpoints never consume probe capacity.
- Probe scheduling is one mechanism for the default and every named profile. Its
  state (round window, exploration cursor and maintained cohort) belongs to the
  probe context: pool, exact engine profile, target ID (which encodes profile name
  and incarnation) and target revision. Gateways are not part of it, so equivalent
  Gateways share one round and one exploration position, and a changed target,
  engine profile or recreated profile starts fresh. A context runs at most one
  round per evidence cadence. A round probes each cohort member once and explores
  at least two further compatible endpoints from the cursor, probing each
  MinSamples times so a challenger can reach M5 evidence in one round. The round
  budget is 64 jobs for the default profile and 30 for a named profile; the cohort
  holds at most the budget minus that exploration reserve (58 and 24 at the current
  M5 defaults). Endpoints that answer join while room remains.
- Health stays an M5 decision. A failed observation never removes a cohort
  member; only a member whose latest M5 explanation is a failure streak or
  unreliability yields its slot to a newly answering endpoint. Members also leave
  when they disappear from the inventory or stop being exact-engine compatible.
  Each Gateway's current selection is pinned in its context's cohort, so an
  endpoint selected after exploration stays maintained and may displace the
  oldest unpinned member; pinned members are never displaced. The pins record
  scheduling demand only; anti-flap state stays in the Gateway's receipt-bound M5
  state. `topN` remains an upper bound: the operator caps it at the context's
  cohort capacity so it never selects endpoints whose evidence it cannot keep
  fresh, and a shortfall shows as `selectedEndpoints` below `topN`.
- The evidence cadence derives from the M5 evidence policy, not the source refresh
  interval: at most `Freshness`, and short enough that MinSamples rounds fit the
  evidence window even when every requeue carries the controller's maximum stable
  10% jitter (five minutes at the current defaults). Gateways requeue at the
  shorter of the cadence and the pool refresh interval. Gateway reconciliation
  reads only the admitted cache or Secret snapshot, so the refresh interval alone
  governs subscription acquisition and no profile starts a separate refresher. With
  eight named profiles, both engines and the default profile the pool ceiling is
  608 jobs per cadence, and the operator shares four probe slots across Gateway
  reconciliations. Probe state is in-memory: a Gateway's pins expire one evidence
  window after its last reconciliation, an unused context after 24 hours, and a
  restart begins empty; durable observations and Gateway selection state are
  unaffected.
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
