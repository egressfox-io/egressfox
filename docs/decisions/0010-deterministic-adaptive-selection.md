# ADR 0010: Deterministic adaptive selection and publication checkpoints

Date: 2026-09-19. Status: accepted.

## Context

M5 must turn M4 facts into a selected endpoint set without confusing missing
evidence with health, carrying evidence across credential or target revisions, or
resetting anti-churn behavior on restart. The same use case must feed both M3
renderers and preserve the file publisher's last-known-good artifact when planning,
validation, publication, or persistence fails.

Raw lowest latency overreacts to small samples and jitter. A weighted collection of
unrelated dimensionless terms would be difficult to explain and tune. A transaction
cannot atomically cover SQLite and the filesystem, so selection state also cannot be
declared committed before the corresponding artifact is known to be published.

## Decision

### Evidence and eligibility

Selection is a pure function of an admitted inventory, revision-specific M4
summaries, an explicit evaluation time, a validated policy, an evidence context and
previous committed decision state. It performs no I/O and reads no clock. The
context contains target ID and confidential target revision, vantage, probe kind and
exact engine profile. A context or connection-revision change starts independently;
old evidence and anti-churn membership are not reused.

All strategies share the same hard gate. A connection is eligible only when it has
fresh evidence for its exact context and revision, at least the configured sample
count, the configured minimum success ratio, a successful latency sample, no active
failure streak, and no unrecovered failure cooldown. Missing, insufficient and stale
evidence have separate safe reason codes. Newly discovered or rotated connections
must first be probed; M4 probes the admitted inventory independently of selection, so
this does not deadlock discovery. No unknown connection is selected as a fallback.

The initial defaults are a 30-minute evidence window, five-minute freshness, three
samples, a 600-per-mille minimum success ratio, and two consecutive failures for
failure eviction. These are bounded, validated operational starting points, not
claims of universal optimality. Operators and experiments may set them explicitly.

### Strategies and score

`static` selects eligible connections in canonical endpoint-ID and confidential
revision order. `lowest_latency` orders them by M4 mean successful request duration,
then canonical connection order. Neither baseline receives adaptive score terms.

`adaptive` uses the same mean successful duration divided by the 95% Wilson lower
confidence bound for the binomial success probability. The result is a conservative
risk-adjusted latency cost in nanoseconds; lower is better. The lower bound makes a
small perfect sample less persuasive than a large perfect sample without inventing
an unrelated weight. The confidence level is algorithm version `adaptive/v1`, not a
tuning knob. Cost and confidence are exposed as bounded integers and comparisons do
not use NaN or map iteration. Wilson's interval is recommended over the simple
normal approximation for binomial proportions by the
[NIST handbook](https://www.itl.nist.gov/div898/handbook/prc/section2/prc241.htm).
M4's explicit rolling window is retained instead of adding EWMA state: it replays
from persisted facts, has defined gap behavior, and follows the guidance that recent
latency estimates be destination-specific without adding an estimator whose benefit
has not yet been measured ([RFC 8085](https://www.rfc-editor.org/rfc/rfc8085)).

Canonical connection order breaks every score tie. Endpoint aliases, provenance,
input ordering and process ordering never break ties.

### Top-N and transitions

Top-N is an ordered set of unique connection revisions. Initial selection takes the
best eligible candidates. If fewer than N are eligible, the result is explicitly
degraded and contains only those candidates. If none are eligible, reconciliation
publishes nothing and retains the existing LKG.

Adaptive reconciliation retains eligible incumbents in their existing positions.
An ordinary challenger may replace the worst unprotected incumbent only after that
incumbent's ten-minute default minimum residence and only when the challenger's cost
is at least ten percent lower. The exact residence boundary permits comparison; the
exact improvement boundary permits replacement. Empty positions are filled without
an improvement test.

An incumbent that disappears or becomes ineligible is replaced immediately when an
eligible candidate exists, bypassing residence and hysteresis. Observed failure
eviction starts a default 15-minute cooldown for that exact connection revision;
after expiry it must show three consecutive fresh successes before it can compete
again. Staleness, disappearance and revision rotation do not create a failure
cooldown. Ordinary optimization does not penalize the evicted candidate. Residence,
hysteresis and failure recovery provide the bounded anti-flapping state; M5 does not
add an opaque transition-frequency penalty before evaluation demonstrates a need.
These failure/recovery concepts are consistent with time-bounded ejection and
consecutive-success recovery used by established data planes such as
[Envoy outlier detection](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/upstream/outlier).

Changing Top-N or any policy parameter changes a deterministic policy fingerprint
and starts a fresh decision for that context. This avoids applying old timers under
new semantics. Default bounds allow Top-N from 1 to 10,000, evidence counts from 1 to
512, durations from zero through documented implementation ceilings, and per-mille
thresholds in their defined ranges; surprising values are rejected, not clamped.

### Explanation and bounded state

A decision contains ordered selected records, per-candidate eligibility reason,
sample/success counts, mean latency, Wilson confidence, adaptive cost, incumbent and
residence flags, transition reason, shortfall and safe aggregate counters. Formatting
and JSON never include credentials, raw configuration, target URLs or confidential
revisions. Metrics dimensions are bounded strategy/reason codes and counts; endpoint
IDs are explanation fields, not metric labels.

Only committed members with their selection times and failure cooldown deadlines are
persisted. Scores are recomputed. State is keyed by a caller-assigned safe gateway
scope, exact evidence context and policy fingerprint. Missing/corrupt state is an
explicit cold start. Endpoint disappearance removes its state, so bounded state is
at most proportional to the current inventory.

### Reconciliation and commit protocol

The reusable standalone use case loads summaries, plans selection, builds an M3
gateway from the selected records, renders, performs the required checker validation,
and publishes through the M3 file boundary. Renderer, validator and publisher errors
do not become health observations. Both Mihomo and sing-box implement the same use
case through their existing profiles.

SQLite schema version 2 adds decision checkpoints to the existing protected local
database. A checkpoint stores a committed or pending decision plus the protected M3
artifact receipt. Apply uses this sequence:

1. render and validate the exact candidate;
2. durably stage the next decision and receipt as pending;
3. publish the validated artifact;
4. promote pending state only when the publisher's current protected receipt matches.

On restart, a matching current receipt promotes a pending checkpoint; a non-matching
pending checkpoint is discarded. A committed checkpoint is used only while its
receipt still matches the current publisher state. Thus a crash before publication
keeps the old decision, while a crash after successful publication completes the
promotion. No cross-resource transaction is claimed. A publish failure preserves
the old artifact and committed state; a post-publish database failure leaves a
recoverable pending checkpoint. Repeated identical desired bytes remain an M3 no-op
while repairing or confirming the checkpoint.

## Alternatives

Selecting unknown endpoints gives immediate output but fabricates trust and can
publish arbitrary credentials. Pure success ratio treats 1/1 and 100/100 equally.
An arbitrary weighted score is harder to interpret than one latency unit adjusted by
a conservative reliability bound. EWMA would add restart state and a decay choice
without evidence that it improves the fixed-window baseline. Persisting scores risks
staleness and duplicate truth. Saving state only after publication without a pending
checkpoint leaves an ambiguous crash window; saving it first can claim an artifact
that was never published. A transition-count penalty is deferred until scenarios
show residence, hysteresis and recovery are insufficient.

## Consequences

M5 decisions are replayable and explainable, and future controllers can call the
same pure selector and orchestration boundaries. A cold database or new revision
cannot produce output until probes establish evidence. The defaults require
controlled evaluation and may change through a versioned decision. The filesystem
still proves publication, not runtime activation. Diversity constraints, adaptive
probe scheduling, destination-wide incident correlation and engine reload remain
future work. Q5 is resolved by this decision.
