# Observations, history, and selection

Status: M4 observation/probe/history and M5 deterministic selection/reconciliation
contracts implemented under ADRs 0009 and 0010. Prometheus export remains future.

M8.5 C1 keeps the complete logical ID/private revision pair as the evidence key.
The [capability gate](engine-protocol-compatibility.md) rejects a valid but
unsupported engine combination before a through-engine probe; no negative health
observation is recorded for that configuration. C2–C4 must filter engine-eligible
candidates before scheduling and selection, keeping capability failure distinct
from network failure and observed target reachability.

## Decisions and scope

Health is evidence about an endpoint reaching a destination from a particular
vantage at a particular time. A single global `healthy` boolean loses that meaning.
P0 includes destination-aware measurement and adaptive selection with history;
P1 M9 exposes multiple named profiles and destination-specific selection policies
over one shared inventory. P0 uses one explicit profile per pool while retaining
the dimensions M9 needs.

EgressFox selects desired pool membership or a preferred endpoint. The engine
still makes every connection-level routing/balancing decision. A small advantage
must not automatically displace a stable incumbent; confirmed unavailability
must permit emergency replacement without waiting for normal residence time.
The accepted M5 formula, eligibility and transition defaults are defined by
[ADR 0010](../decisions/0010-deterministic-adaptive-selection.md).

## Probe evidence

Record the full endpoint connection identity (logical ID plus confidential revision),
profile/target revision, execution vantage, probe kind, start/completion time,
duration, and a bounded outcome/reason. The summary key prevents mixing different
paths, credentials, targets, or methods. M4 exposes narrow protected persistence
boundaries for confidential connection and target revisions; diagnostics use only
safe logical IDs.
DNS, direct TCP, TLS, HTTP, and through-endpoint checks answer different questions.
A successful TCP dial to the proxy is not proof it can reach a destination.

| Evidence | Example interpretation |
| --- | --- |
| Successful through-endpoint HTTPS response matching criteria | This endpoint reached this target using this execution path |
| Direct TCP connect to endpoint succeeded | Endpoint address accepted a TCP connection only |
| DNS resolution failed | Classify which resolver/path failed; not proof of universal endpoint failure |
| Probe worker timed out before starting a queued job | Scheduler/observer limitation; not a network failure sample |
| No sample yet or expired evidence | Unknown/stale; not zero latency, success, or observed failure |
| Target fails across many endpoints and the control path | Suspected target/observer incident; avoid automatically condemning every endpoint |

M4 observations contain end-to-end bounded request duration, outcome and an optional
safe HTTP status. Summaries contain sample/success counts, success ratio, latest
sample/success, mean successful duration, trailing success/failure streaks and
explicit freshness. Connect/TTFB breakdowns, exit IP, country/ASN, EWMA, jitter and
persisted transition-frequency penalties remain future
work and require defined units, windows, sample counts and denominators.
Do not call HTTP latency variation packet loss or invent an ICMP metric from it.
No response bodies or authentication data belong in ordinary observation records.

### Through-endpoint execution

The implemented path runs an isolated, pinned engine with an endpoint-specific
outbound and loopback SOCKS listener, then executes one bounded HTTP GET through it.
The engine implements authentication, transport, tunneling and SOCKS. This remains
separate from the production gateway, and no fallback or direct route can masquerade
as success.

M4 chooses one isolated pinned engine process per endpoint/revision and request.
It renders the M3 one-endpoint gateway, validates exact bytes, waits for a private
loopback SOCKS listener, executes one authorized bounded HTTP request, and terminates
and waits for the engine. Rendering, validation, process/readiness, resolution and
authorization failures produce execution errors rather than endpoint observations.
This deliberately favors attribution and cleanup over startup efficiency.

HTTPS is the default; plain HTTP, private/loopback endpoints and private/loopback
targets require independent explicit authorization. The executor resolves endpoint
and target names locally, rejects either full answer set if any address violates
policy, sorts allowed addresses, and renders/dials execution-only literals while
retaining original HTTP Host and TLS SNI. It rejects redirects, environment proxies,
link-local/multicast/unspecified addresses and untrusted private/shared/control space,
and bounds readiness, request duration and response bytes. Success, request
timeout, connection failure, TLS failure, unexpected status/redirect and oversized
response are observations after request start. Resolution, authorization, rendering,
native validation, process/readiness and scheduler failures are safe execution errors.

Probe vantage matters: an operator Pod may have a different route than a gateway
Pod or standalone host. P0 must declare its vantage and limits. Remote/distributed
probing is not assumed. Engine-local URL tests are a different observation source
unless they provide the identity, target, and timing semantics the core requires.

### Bounded scheduling

M4 uses a bounded queue, maximum job count, global worker count, per-logical-endpoint
limit and per-target limit. Defaults are 256 queued jobs, 10,000 total jobs, four
workers, one job per logical endpoint and two per target. Result slots preserve input
order; cancellations create no observation. Rate limiting, staggering, retries,
adaptive priority and exploration policy remain M5 or later work.

Capacity estimates must consider `endpoints × profiles ÷ interval` plus retry and
recovery traffic. The Cartesian product must never translate into an unbounded
goroutine count. Maximum response bytes, test duration, bytes transferred, active
engine workers, and queue age are part of capacity control. Overload should produce
visible skipped/deferred observations, not fabricated failure streaks. Prevent
engine-native health tests and EgressFox probes from multiplying load invisibly.

## Historical state

[ADR 0004](../decisions/0004-sqlite-history.md) chooses SQLite for initial standalone
persistence and [ADR 0009](../decisions/0009-bounded-probes-and-sqlite-evidence.md)
fixes the adapter: ncruces/go-sqlite3 v0.35.5, WAL with FULL
synchronization, one connection/writer, explicit migration and transactional age,
per-key and global retention. The database directory and files are a confidential
same-host boundary. Unknown future schema versions and inaccessible/corrupt state
fail instead of silently falling back to memory.

The adapter persists an immutable observation idempotently, prunes retention in
the same transaction, loads an exact complete-key time window, and derives the same
domain summary after restart. SQL and schema migration stay in the adapter; there is
no generic CRUD repository or ORM.

Schema v1 retains only immutable raw samples partitioned by complete evidence key.
Defaults enforce 30 days, 512 rows per key and 100,000 rows globally. It stores
private fixed-size connection/target revisions, never subscription bodies, target
URLs, credentials or rendered configurations. M5 migrates to schema v2 with at most
one committed and one pending protected selection checkpoint per gateway scope.
Checkpoints hold exact context/revisions, policy fingerprint, selected-at times,
bounded cooldowns and an M3 protected receipt. Derived scores are never persisted.

Sample digests make replay idempotent. Explicit evaluation times make age pruning,
query windows and freshness deterministic; late samples remain immutable facts and
summary ordering uses completion time plus sample ID. Trailing streaks are derived
from that order. M5 persists the minimum anti-churn state so restart does not reset
residence or cooldown.

Database loss is an explicit cold start. Do not pretend unknown history is healthy.
An inaccessible/corrupt database must not replace an output with an empty artifact.
Recovery, migration failure, retention, disk-full, and cancellation behavior need
integration tests. Do not silently fall back to memory and claim persistence.

SQLite is not an HA coordination system. Its [WAL documentation](https://sqlite.org/wal.html)
requires a same-host shared-memory arrangement and permits one writer at a time.
It is not a shared network-filesystem solution for operator replicas. PostgreSQL
is a P2 option; operator HA remains a separate design problem.

## Eligibility before scoring

M5 applies one common evidence gate before every strategy: exact connection and
context, a fresh summary, minimum sample and success thresholds, at least one
successful latency, no configured failure streak, and no active/unrecovered
failure cooldown. Missing, insufficient, stale, unreliable, failed, cooldown and
recovery cases have separate safe reason codes. Unknown revisions require probes;
they are not selected with a fabricated zero score. M4 can probe the complete
admitted inventory independently, so selection is not required for exploration.

Future static source/country/ASN/tag policy restrictions also belong before ranking.
Hard policy restrictions cannot be traded for a higher score. Untrusted metadata
must retain provenance.

Top-N selects an ordered set of unique exact connection revisions up to the requested
bound. A positive shortfall is published as an explicit degraded result; zero
eligible candidates retain LKG and publish nothing. No unhealthy repetition or
direct fallback fills the pool. Endpoint ID plus private revision provides the
canonical tie-break without exposing the revision.

M5 implements only `static`, `lowest_latency`, and `adaptive`. Static uses canonical
connection order after eligibility. Lowest latency uses mean successful duration,
then canonical order. Adaptive uses the risk-adjusted cost below. Random, weighted,
availability-only and `all` values are not accepted enum placeholders.

## Adaptive selection

Algorithm `adaptive/v1` calculates the 95% Wilson lower bound `L` for successful
samples and ranks by `mean successful duration / L`; lower is better. Confidence is
reported in parts per million and cost in nanoseconds. The selector uses exact
rational integer comparison for the relative replacement boundary, so its result is
not sensitive to displayed cost rounding. It uses the explicit M4 rolling window
rather than adding EWMA state.

The initial validated defaults are 30-minute history, five-minute freshness, three
samples, 600-per-mille success, two trailing failures, ten-minute residence,
ten-percent required improvement, 15-minute failure cooldown and three trailing
successes for recovery. Configuration bounds are enforced and surprising values are
rejected rather than clamped. A policy change changes the fingerprint and starts a
fresh decision context.

Eligible incumbents retain their order. Ordinary replacement waits for residence
and the relative improvement boundary. Disappearance, stale/invalid evidence or
confirmed failure bypasses those gates. Only observed failure/unreliability starts
cooldown; source disappearance and credential rotation do not. A revision, target,
vantage, probe-kind or engine-profile change never inherits incompatible evidence.
No additional flapping score is used until evaluation shows a need beyond these
interpretable mechanisms.

Destination failures and global gateway failures require separate detection scopes.
Avoid two competing optimization loops: EgressFox may manage pool membership while
an explicitly requested engine URL-test group picks active members. Explain which
loop made a switch; do not attribute every data-plane failover to EgressFox.

Possible future diversity adds constraints such as `maxNodes: 6`, `maxPerCountry: 2`,
`maxPerASN: 1`, `maxPerSource: 3`. These are illustrative. Define whether limits
are hard, how unknown/multi-source values count, and what an infeasible result
means before selecting an optimization algorithm. Never silently relax constraints.

The committed P1 path does not schedule diversity. Source provenance exists today,
but country/ASN/provider enrichment does not, and one endpoint may have multiple
sources. A source-only constraint remains an optional P1 candidate after Q10 resolves
attribution and M9 establishes named profiles; geography/ASN diversity is later work.

## Explainability and observability

M5 decisions preserve compact reason codes, algorithm/version through the policy
fingerprint, explicit evaluation time, relevant normalized signals, exclusions,
prior selection, and switch category. This enables later P1 `explain`/diff tools
without keeping all raw history in memory. Reasons must distinguish policy denial,
unsupported protocol, stale evidence, recovery cooldown, and insufficient capacity.
Log neither raw input nor engine config; safe error construction precedes logging.

Structured logs identify stages, safe operation IDs, duration, and bounded reasons.
Kubernetes translates outcomes into Conditions; standalone reports the same
underlying failure classes. Use counters for events, gauges for current counts,
and histograms for duration with explicit units. Future families include:

| Family | Intended scope |
| --- | --- |
| `egressfox_source_*` | Fetch outcome, age, admitted/rejected counts |
| `egressfox_endpoint_*` | Inventory and eligibility counts |
| `egressfox_probe_*` | Outcomes, latency, queue depth, deferred checks |
| `egressfox_selection_*` | Decision duration and change reasons |
| `egressfox_render_*` | Duration and bounded validation/capability failures |
| `egressfox_publish_*` | Attempts, errors, and published-generation age |
| `egressfox_failover_*` | Control-plane switch counts and detection-to-publication timing |

M5 exposes bounded decision counts and replay reports in-process, including eligible,
selected, degraded, change, emergency and no-feasible results. The table remains
namespacing intent for a future metrics exporter. End-to-end recovery time
requires traffic measurements; publication timing alone is not failover time.
Default labels should be bounded enums such as engine, stage, result, and reason.
No endpoint IDs, raw hosts, URIs, destinations, external IPs, content digests, or
arbitrary error messages as labels. Even pool/source labels need a budget and
series-removal policy. Authenticated diagnostics can provide per-endpoint detail.
P1 M11 implements the bounded metrics adapter; M12 may expose only the latest
deterministically truncated decision report through an authorized Kubernetes-native
boundary after Q17. It does not store observation history in CR status.
See [Prometheus naming guidance](https://prometheus.io/docs/practices/naming/).

## Non-goals and open questions

No target availability guarantee, distributed probe fleet, predictive model,
arbitrary user scoring code, metrics exporter or observability stack is implemented.
Q5 is resolved by ADR 0010. Reproducible replay
and traffic experiments are specified in the [test strategy](../development/testing.md).
