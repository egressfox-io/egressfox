# ADR 0009: Isolated engine probes and bounded SQLite evidence

Date: 2026-09-19. Status: accepted.

## Context

M4 must measure an exact endpoint connection revision against an exact destination
from a declared vantage. Reusing a production gateway or an engine group with
fallback would make attribution ambiguous. Implementing VLESS, Trojan, TLS tunneling
or SOCKS inside EgressFox would cross the control-plane boundary. History must survive
restart without making confidential connection revisions, target URLs or raw engine
output ordinary diagnostics.

SQLite is already selected for initial standalone history by ADR 0004, but its
driver, schema, durability, migration and retention contracts were deliberately
deferred. The store must support deterministic M5 evidence without becoming a
generic persistence framework or an operator HA database.

## Decision

### Probe execution

Each M4 probe uses one isolated pinned Mihomo or sing-box process and one endpoint
record. EgressFox builds the existing M3 gateway model with only that connection
revision, renders and natively validates the complete configuration, starts it in a
private temporary workspace, waits for its loopback SOCKS listener, performs one
bounded request, then terminates and waits for the process. There is no fallback,
direct route or engine-native health test. Failure to render, validate, start or keep
the local engine alive is an execution failure and produces no endpoint observation.

The initial probe kind is an HTTP GET through the local SOCKS listener. HTTPS is the
safe default. Plain HTTP, private/loopback endpoints and private/loopback targets
require separate explicit trusted-input options. EgressFox resolves both the endpoint
and target locally, applies destination policy to every result, and rejects the whole
answer set if any address is denied. It chooses a deterministic allowed address and
renders/dials an execution-only literal; endpoint TLS and target HTTPS retain their
original hostnames for SNI, and target HTTP retains its original Host.
Redirects are rejected, environment proxy variables are ignored, response bytes are
bounded, and success requires the configured exact status plus a body that reaches
EOF within the limit. The measured duration spans request execution through bounded
body completion and uses Go's monotonic elapsed time.

A target has a caller-assigned safe logical ID and a confidential deterministic
revision over its normalized URL, expected status and response bound. Observations
store the target ID and revision, never the URL. A vantage is a caller-assigned safe
ID identifying the standalone execution location/configuration; remote fleet
probing is not implied. The engine compatibility profile is also part of the evidence
key. Connection, TLS, timeout, unexpected-status and oversized-response outcomes are
valid target-specific observations once the ready engine starts the request.
Resolution/authorization, scheduler cancellation and local engine failures remain
safe structured execution errors rather than negative health evidence.

Scheduling is independent from probe semantics. M4 uses a bounded worker pool and
bounded queue with explicit maximum jobs, global concurrency, and per-logical-endpoint
and per-target concurrency. Input-order result slots make completion deterministic.
Queue cancellation creates no observation. There are no retries, jitter, adaptive
priorities or selected-endpoint feedback in M4. Default budgets are 4 workers, a
256-job queue, one active job per logical endpoint, two per target and 10,000 jobs
per run; callers may lower them, while implementation ceilings prevent accidental
unbounded configuration.

### Observation and history

An immutable observation binds logical endpoint ID, confidential connection
revision, target ID/revision, vantage ID, probe kind, exact engine profile, UTC start
and completion timestamps, monotonic duration, outcome and bounded safe response
metadata. Endpoint source provenance is not part of measurement identity. A stable
sample digest makes repeated insertion idempotent; late samples remain facts and do
not mutate earlier rows.

M4 uses `github.com/ncruces/go-sqlite3` v0.35.5 through `database/sql`. It is a
maintained CGo-free SQLite distribution with a narrow driver surface and supports the
project's Linux/macOS amd64/arm64 targets without system SQLite version skew or a
cross-compilation C toolchain. `mattn/go-sqlite3` is mature but requires CGo;
`modernc.org/sqlite` is also viable but has a substantially broader generated
dependency surface for this use case. No ORM is used.

Schema version 1 is installed transactionally from SQLite `user_version=0`; a newer
unknown version is rejected. The database uses WAL, `synchronous=FULL`, foreign keys,
a bounded busy timeout and one `database/sql` connection/writer. It is a local
same-host file, never a network-filesystem or multi-replica coordination mechanism.
The parent directory must already be private and non-symlinked; database, WAL and SHM
files share that protected boundary and the main database is forced to `0600`.

Raw history is bounded by all three limits: 30 days, 512 observations per complete
evidence key and 100,000 rows globally. Pruning occurs in the same transaction as an
insert and uses the caller's explicit evaluation time. These conservative defaults
retain multiple daily windows for early experiments while bounding a standalone
database; benchmark evidence may justify later revision. Database loss is an
explicit cold start. Open, migration and corruption failures are returned; there is
no silent in-memory fallback. M4 does not automate backups: operators must stop the
single writer or use a SQLite-aware snapshot that includes WAL state.

Queries require the complete evidence key and an explicit UTC window. A baseline
summary reports sample/success counts, success ratio, latest observation, latest
success, mean successful end-to-end duration and freshness of the latest sample at
an explicit evaluation time. Retention and freshness are independent. No score,
ranking, percentile, hysteresis or endpoint selection is persisted or computed.

## Alternatives

A shared engine is cheaper but requires a control API and generation/fallback proof
to attribute every request; process-per-probe is simpler and bounded for M4. Direct
protocol clients would duplicate the data plane. Letting the proxy resolve arbitrary
target hostnames would weaken address authorization. Storing only logical endpoint
IDs would reuse old credential health. Storing raw URLs or engine errors would expand
the secret and injection surface. Memory history would lose restart/replay behavior.
Unlimited rows or age-only retention would remain unbounded across many identities.

## Consequences

M5 can query fresh evidence for an exact connection revision, target, vantage, kind
and engine without importing SQL. Credential or target rotation immediately creates
a distinct evidence key. Process startup cost limits throughput, so the scheduler
budgets engine processes explicitly; M5 may later prioritize work without changing
observation semantics. Target-wide incident correlation, richer probe profiles,
shared engine workers, automated backups and distributed vantage identity remain
future work. Q3 and Q4 are resolved by this decision.
