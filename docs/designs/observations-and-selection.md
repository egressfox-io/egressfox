# Observations, history, and selection

Status: M4 observation, probe and history contract implemented under ADR 0009;
adaptive selection and exported metric instruments are not implemented.

## Decisions and scope

Health is evidence about an endpoint reaching a destination from a particular
vantage at a particular time. A single global `healthy` boolean loses that meaning.
P0 includes destination-aware measurement and adaptive selection with history;
P1 expands to multiple profiles and destination-specific selection policies.
P0 can start with one explicit profile per pool while retaining these dimensions.

EgressFox selects desired pool membership or a preferred endpoint. The engine
still makes every connection-level routing/balancing decision. A small advantage
must not automatically displace a stable incumbent; confirmed unavailability
must permit emergency replacement without waiting for normal residence time.
No scoring formula, weight, timeout default, or threshold is accepted yet.

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
safe HTTP status. M4 summaries contain sample/success counts, success ratio, latest
sample/success, mean successful duration and explicit freshness. Connect/TTFB
breakdowns, exit IP, country/ASN, EWMA, jitter, streaks and flapping remain future
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
fixes the M4 adapter: ncruces/go-sqlite3 v0.35.5, schema version 1, WAL with FULL
synchronization, one connection/writer, explicit migration and transactional age,
per-key and global retention. The database directory and files are a confidential
same-host boundary. Unknown future schema versions and inaccessible/corrupt state
fail instead of silently falling back to memory.

The M4 adapter persists an immutable observation idempotently, prunes retention in
the same transaction, loads an exact complete-key time window, and derives the same
domain summary after restart. SQL and schema migration stay in the adapter; there is
no generic CRUD repository, ORM, selection-state table or publication receipt here.

Schema v1 retains only immutable raw samples partitioned by complete evidence key.
Defaults enforce 30 days, 512 rows per key and 100,000 rows globally. It stores
private fixed-size connection/target revisions, never subscription bodies, target
URLs, credentials or rendered configurations. Selection state, streaks, cooldown,
score and decision history remain M5 decisions.

Sample digests make replay idempotent. Explicit evaluation times make age pruning,
query windows and freshness deterministic; late samples remain immutable facts and
summary ordering uses completion time plus sample ID. M5 must separately define and
persist enough anti-flapping/selection state that restart does not trigger churn.

Database loss is an explicit cold start. Do not pretend unknown history is healthy.
An inaccessible/corrupt database must not replace an output with an empty artifact.
Recovery, migration failure, retention, disk-full, and cancellation behavior need
integration tests. Do not silently fall back to memory and claim persistence.

SQLite is not an HA coordination system. Its [WAL documentation](https://sqlite.org/wal.html)
requires a same-host shared-memory arrangement and permits one writer at a time.
It is not a shared network-filesystem solution for operator replicas. PostgreSQL
is a P2 option; operator HA remains a separate design problem.

## Eligibility before scoring

Apply static criteria (protocol, source, country/ASN, IP family, tags, alias,
allow/deny) and freshness/health constraints before ranking. Untrusted metadata
must carry provenance. Hard policy restrictions cannot be traded for a higher
score. Latency and historical availability filters need explicit treatment for
insufficient samples and unknown values.

P0 Top-N means select up to the requested bound from eligible endpoints, with
deterministic tie-breaking. It does not authorize unsafe repetition or fallback
when fewer candidates exist. A policy must define whether a smaller set is usable,
and publication must handle no feasible set explicitly. The proposed default is
retain LKG and report degraded/unready; direct fallback requires explicit intent.

Potential strategies are all, seeded random, lowest latency, highest availability,
weighted, and adaptive. Only implemented, validated strategies belong in a public
enum. Random strategy must persist or explicitly derive its seed/epoch so retries
do not continually churn the configuration.

## Adaptive selection research

Treat the following as mechanisms to evaluate, not an accepted formula:

- Smooth latency with a documented EWMA and availability windows.
- Compare an eligible challenger with the incumbent using an improvement threshold
  and hysteresis; distinguish absolute and relative improvements.
- Enforce minimum residence time for ordinary switches and failure penalties or
  cooldown to avoid flapping; persist state across restarts.
- Require evidence of recovery before re-entry, while continuing bounded exploration.
- Bypass ordinary residence/improvement gates for an incumbent that meets the
  configured failure criteria. No eligible replacement means a visible shortfall.
- Treat unavailable/unknown latency as missing evidence, never as a superior score.

Example acceptance scenarios: a challenger 2 ms faster does not *necessarily*
replace a stable endpoint; a confirmed failed selected endpoint can be replaced
at the next decision without residence delay; a recovering endpoint does not
oscillate immediately back into service. These describe properties, not constants.

Destination failures and global gateway failures require separate detection scopes.
Avoid two competing optimization loops: EgressFox may manage pool membership while
an explicitly requested engine URL-test group picks active members. Explain which
loop made a switch; do not attribute every data-plane failover to EgressFox.

P1 diversity adds constraints such as `maxNodes: 6`, `maxPerCountry: 2`,
`maxPerASN: 1`, `maxPerSource: 3`. These are illustrative. Define whether limits
are hard, how unknown/multi-source values count, and what an infeasible result
means before selecting an optimization algorithm. Never silently relax constraints.

## Explainability and observability

P0 internal decisions should preserve compact reason codes, algorithm/version,
input revision/cutoff, relevant normalized signals, eligibility exclusions,
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

These are namespacing intentions, not existing metrics. End-to-end recovery time
requires traffic measurements; publication timing alone is not failover time.
Default labels should be bounded enums such as engine, stage, result, and reason.
No endpoint IDs, raw hosts, URIs, destinations, external IPs, content digests, or
arbitrary error messages as labels. Even pool/source labels need a budget and
series-removal policy. Authenticated diagnostics can provide per-endpoint detail.
See [Prometheus naming guidance](https://prometheus.io/docs/practices/naming/).

## Non-goals and open questions

No production score weights, target availability guarantee, distributed probe
fleet, predictive model, arbitrary user scoring code, or observability stack is
implemented. Q5 in the [decision queue](../decisions/open-questions.md) gates
adaptive scoring. Reproducible replay
and traffic experiments are specified in the [test strategy](../development/testing.md).
