# ADR 0024: Target-aware profiles over a shared pool inventory

Date: 2026-09-25. Status: Accepted for M9 implementation; scheduling amended
2026-10-01 by the final M9 scheduler hardening, including shared-capacity
reservation, round ordering and `topN` diagnostics.

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
- Each Gateway has scheduling demand in the context: the selection it last
  published there. Retaining a published last-known-good is separate from
  protecting its probe capacity: demand protects an endpoint only while M5 has
  not rejected it for its failure streak or unreliability. Missing or withheld
  evidence, probe deferral, infrastructure errors and another Gateway's cooldown
  are not rejections, and one failed observation never reaches one. A rejected
  endpoint stays in the published output until its Gateway publishes a
  replacement, but yields its cohort slot to an answering endpoint and is then
  observed again only when exploration reaches it.
- Before rendering, a non-empty decision is reserved. Because any write can end
  with an uncertain outcome, the reservation must fit the cohort with every
  protected demand, including the Gateway's own current output, plus the new
  selection: both plausible outputs have to stay maintainable before anything is
  written. When that does not fit, the reconciler plans once more with evidence
  withheld from endpoints outside the cohort, so M5 chooses among maintained
  endpoints; if even that is refused, nothing is written and the
  last-known-good remains. A Gateway whose own healthy output fills the cohort
  therefore cannot move to an endpoint outside it until a protected endpoint is
  released; rejected endpoints release theirs, so failover still proceeds. A
  failure before any external write releases the reservation. A confirmed
  publication with a committed checkpoint makes it the Gateway's demand. A
  write that happened or may have happened without that confirmation (an
  ambiguous publisher error, a failed receipt read-back, or a failed checkpoint
  commit) is held as uncertain demand: the current output stays the Gateway's
  demand and in the cohort, the attempted endpoints are admitted into rejected
  or unprotected slots that the reservation proved available, both are probed
  every round, and nothing is rolled back externally. While the hold lasts, no
  further attempt is reserved.
- Before its next probe round, the Gateway reads the actual current output: for
  BYO, the owned output Secret's receipt; for a managed Gateway, the receipt of
  the generation its status records as published, which is what the runtime
  activates. A generation Secret written by a failed operation is not current
  until the status records it. If the current output carries the attempted
  receipt, the attempt becomes the Gateway's demand; otherwise it is dropped and
  the current output, which stayed maintained, remains. An unreadable output
  keeps both. The store's receipt-bound checkpoint recovery decides M5 state
  independently and is never reported as committed when the commit failed.
  Status reports `PublicationUnconfirmed` while a write is unresolved. Demand
  expires one evidence window after the Gateway last reconciled in the context
  and is dropped as soon as it reconciles in another. Anti-flap state stays in
  the Gateway's receipt-bound M5 state.
- `topN` remains an upper bound. M5 receives `topN` capped at the context's cohort
  capacity, so it never selects endpoints whose evidence the scheduler cannot
  keep fresh; the operator keeps the requested value and reports the shortfall
  through the `SelectionReady` reason: `ProbeCapacityLimited` for the cap,
  `ProbeCapacityShared` for a decision constrained by other Gateways' demand,
  `InsufficientEligibleEndpoints` for missing evidence, and
  `ProbeCapacityExceeded` when no maintainable decision could be reserved. While
  healthy protected demand, including a Gateway's own current output, leaves no
  room for its transition, a better endpoint outside the cohort is not adopted
  until some protected endpoint is released.
- The evidence cadence derives from the M5 evidence policy, not the source refresh
  interval: at most `Freshness`, and short enough that MinSamples rounds fit the
  evidence window even when every requeue carries the controller's maximum stable
  10% jitter (five minutes at the current defaults). Gateways requeue at the
  shorter of the cadence and the pool refresh interval. Gateway reconciliation
  reads only the admitted cache or Secret snapshot, so the refresh interval alone
  governs subscription acquisition and no profile starts a separate refresher. With
  eight named profiles, both engines and the default profile the pool ceiling is
  608 jobs per cadence, and the operator shares four probe slots across Gateway
  reconciliations. Gateway reconciliations run serially, so a round completes
  before another Gateway reads its evidence; a concurrent caller would read the
  previous round's. Probe state is in-memory: an unused context expires after 24
  hours, and a restart begins with empty cohorts and demand while durable
  observations and Gateway selection state are unaffected. After a restart a
  published selection is protected again at the Gateway's first successful
  publication; until then its endpoints are probed only if exploration reaches
  them, so an outage longer than `Freshness` can make M5 replace an incumbent
  once.
- A round runs exploration first and maintenance last, with demanded members at
  the end, so maintenance evidence is the newest at evaluation. New exploration
  probes start only within half of `Freshness` after the round starts; later
  exploration jobs are deferred and record nothing. Maintenance is not cut short.
  Freshness at evaluation therefore holds while the maintenance phase completes
  within `Freshness`: at most 58 or 24 probes at a per-target concurrency of two.
  A longer phase is reported as `ProbeRoundOverloaded`; demanded members still
  run last. A round that fails before its evidence is stored, including
  cancellation and evidence-store errors, is released for the next
  reconciliation and restores its exploration position.
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
