# Observations, history, and selection

Status: required behavioral properties and proposed design; algorithms, storage
schema, probe execution, and metric instruments are not implemented.

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

Record endpoint connection revision, profile/target revision, execution vantage,
probe kind, start/completion time, duration, and a bounded outcome/reason. The
summary key must prevent mixing different paths, credentials, targets, or methods.
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

Observations may contain connect latency, TTFB, total request time, result, exit
IP, and measured country/ASN. Derived summaries may contain EWMA latency, rolling
availability, jitter, failure/recovery streaks, flapping, and freshness. Define
units, windows, sample counts, and denominators before implementing each field.
Do not call HTTP latency variation packet loss or invent an ICMP metric from it.
No response bodies or authentication data belong in ordinary observation records.

### Through-endpoint execution

Proposed direction: run/use an isolated, pinned engine with an endpoint-specific
outbound and a controlled local proxy/API interface, then execute a bounded probe
through it. The engine implements authentication, transport, and tunneling. Keep
this separate from the production gateway so probes cannot change user traffic.
The execution adapter must prove which endpoint was used; no automatic fallback
to another endpoint or direct connection may masquerade as success.

Before choosing process-per-endpoint, batching in a reusable engine, or another
topology, measure startup cost, concurrency isolation, credential lifetime,
supported probe types, engine API exposure, and cancellation/cleanup behavior.
This is Q3 and an M4 entry gate. Do not build protocol clients to bypass it.

Probe vantage matters: an operator Pod may have a different route than a gateway
Pod or standalone host. P0 must declare its vantage and limits. Remote/distributed
probing is not assumed. Engine-local URL tests are a different observation source
unless they provide the identity, target, and timing semantics the core requires.

### Bounded scheduling

Use a bounded queue and global concurrency/rate budgets plus per-source/provider,
target, and endpoint limits where needed. Stagger checks; prioritize a bounded
set of selected endpoints while keeping exploration capacity for recovery and
unknown candidates. Use deadlines, backoff, cancellation, and bounded retries.

Capacity estimates must consider `endpoints × profiles ÷ interval` plus retry and
recovery traffic. The Cartesian product must never translate into an unbounded
goroutine count. Maximum response bytes, test duration, bytes transferred, active
engine workers, and queue age are part of capacity control. Overload should produce
visible skipped/deferred observations, not fabricated failure streaks. Prevent
engine-native health tests and EgressFox probes from multiplying load invisibly.

## Historical state

[ADR 0004](../decisions/0004-sqlite-history.md) chooses SQLite for initial standalone
persistence. Use a local file with controlled ownership, explicit migrations,
transactional updates, and bounded retention. Evaluate the driver, CGO tradeoff,
journal mode, busy handling, and backup/recovery with real write rates before M4.
No driver or generic storage interface has been selected.

Organize storage around real operations: persist an observation and its summary
consistently, load a coherent decision snapshot, record selection transitions,
and record publication receipts. Keep SQL and migrations in the adapter. Do not
introduce a generic CRUD repository, ORM, or PostgreSQL-shaped abstraction now.

Potential retained facts include first/last seen, last success/failure, rolling
availability, EWMA and jitter, streaks, flapping state, selected-since/cooldown,
score history, selection history, and compact decision reasons. Partition by the
observation dimensions; retain bounded raw samples separately from aggregates.
The specific schema and retention budgets are open. Store secret references where
possible, not copied subscription bodies or rendered credential-bearing configs.

Persist enough anti-flapping state that restart does not reset residence time and
trigger churn. Define wall-clock jump handling and how durations resume after
restart. Inject evaluation time in tests. Late/duplicate observations must not
move summary time backwards or increment streaks twice. Decide sample IDs and
out-of-order processing in the storage/observation milestone.

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
implemented. Q3–Q5 in the [decision queue](../decisions/open-questions.md) gate
probe topology, persistence semantics, and adaptive scoring. Reproducible replay
and traffic experiments are specified in the [test strategy](../development/testing.md).
