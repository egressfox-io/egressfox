# Testing and reproducible evaluation

Status: testing strategy. M1 implements endpoint identity and inventory tests. M2
adds bounded HTTP, parser/admission, snapshot, leakage and URI fuzz tests. M3 adds
reviewed renderer goldens, exact-profile native validation, publication failure and
recovery tests, and an opt-in controlled sing-box traffic smoke. Commands available
now are listed in [workflow](workflow.md). M4 adds resolver/authorization, scheduler
budget, through-engine observation, SQLite migration/restart/retention and summary
tests. Do not claim M5–M6 selection, controller or cluster tests run before those
milestones implement them.

## Layers and gates

| Layer | Purpose and representative cases | Introduce |
| --- | --- | --- |
| Unit and property tests | Identity equivalence/distinction, provenance union, stable ordering; policy references; selection invariants with fake clock/seed | M1 onward |
| Parser and input fuzzing | Bounded malformed/oversized inputs, duplicate/ambiguous fields, encoding/depth limits, sanitized error paths | M2 |
| Renderer golden tests | Exact reviewed Mihomo and sing-box outputs for canonical common-model fixtures; unsupported features and ordered rules | M3 |
| Native engine validation | Golden output accepted by the exact pinned engine/version/build; wrong version/unsupported feature rejection | M3 |
| File publication integration | Invalid generation retains LKG, no-op equality, stale work, disk-full/permission/symlink/conflict/crash-recovery boundaries | M3 |
| Network and SQLite integration | Local controlled destinations, through-engine endpoint attribution, cancellations/budgets, transactions/migrations/restarts/retention | M4 |
| Selection replay and scenario tests | Stable ties, unknown/stale evidence, ordinary residence, emergency replacement, recovery, no feasible candidates, restart continuity | M5 |
| Kubernetes envtest | API validation/defaults/status, indexed watches, Secret rotation, retries/conflicts, ownership and idempotency | M6 |
| Real-cluster tests using kind | Helm install/upgrade, RBAC, garbage collection, volume/Secret consumption, operator restart, BYO lifecycle | M6 |
| End-to-end traffic | Generated engine actually proxies controlled workload; wrong routes, selective destination failures, failover and recovery | M3 small smoke; M5/M6 full scenarios |

No fixed coverage percentage substitutes for verifying failure boundaries. Tests
must be offline/local by default; external-provider/live-network tests, if added,
require explicit opt-in and test data. Avoid live public API availability as a CI
dependency. Always use synthetic credentials and controlled targets.

## Determinism and boundaries

Pass explicit time, deterministic seeds, historical snapshots, and input revisions
into pure decision functions. Permute input map/list order where order is not
semantic. Preserve order where it is semantic. Replaying an identical snapshot must
produce the same selected IDs/order, reasons, and bytes. Test cold starts, clock
jumps, late/duplicate samples, credential/profile revision changes, and restart
restoration of anti-flapping state.

Use real SQLite in temporary directories for storage contracts, not only mocked
repository calls. Force failures at publication boundaries and verify actual old
bytes/receipts survive. Concurrency tests should include an old slow generation
finishing after a new one and cancellation under leadership loss. Race detection
is already enabled in `make test`.

Test SSRF/dial policy with local fixtures/fake resolvers, redirect credential rules,
bounded parser/resource behavior, and redaction canaries. Do not merely assert that
a redaction function works; exercise errors, logs, metrics labels, Conditions,
Events, explain output, and validator diagnostics where applicable.

## Golden and compatibility discipline

Keep testdata next to the owning package when it exists. Each fixture should state
common-model intent, expected engine/version profile, supported protocols/options,
expected artifact and expected rejection if unsupported. Names and values are
synthetic. Do not include operational subscription files.

Golden updates are deliberate reviewed changes, never automatic approval because
a test regenerated them. Review semantic differences, rule order, defaults, tags,
secret-bearing fields, and rejected behavior. Native validation and controlled
traffic tests complement golden bytes; neither proves all production behavior.

Pin engine executable versions/checksums or image digests and validator build flags
with the test harness. Upgrade one tested matrix deliberately; do not use `latest`
in compatibility tests. Golden rendering must not depend on wall-clock timestamps,
map iteration, a developer's home directory, or remote rule-set changes. Auxiliary
files need pinned fixtures and explicit ownership.

M3 native tests are opt-in so ordinary tests stay offline. Set
`EGRESSFOX_MIHOMO_BINARY` and `EGRESSFOX_SINGBOX_BINARY` to absolute paths for
`TestPinnedNativeValidation`; the sing-box variable also enables
`TestSingBoxPublishedArtifactCarriesControlledTraffic`. The harness verifies the
configured profile itself and rejects a version mismatch. Only use official,
checksum-verified binaries matching [ADR 0008](../decisions/0008-engine-artifacts-and-file-publication.md).

M4's `TestPinnedEnginesProduceControlledObservations` uses the same variables and
requires sing-box for its controlled local TLS Trojan server. It natively validates
and runs each available pinned client, then requires a successful bounded HTTP
observation through that exact one-revision path. The fixture is opt-in, loopback-only,
uses synthetic credentials, and implements no proxy protocol in EgressFox.

## Kubernetes tests

Fake clients can test small code paths but do not establish API-server semantics.
Use envtest for schema/default/status and reconciliation integration after real
scaffolding. The official [envtest guide](https://book.kubebuilder.io/reference/envtest)
explains that it runs an API server and etcd without built-in controllers/kubelet.
It cannot prove garbage collection, scheduling, mounted Secret propagation, or
real runtime connectivity. Those require kind or another real cluster.

Match envtest tooling/assets to the pinned generated controller-runtime/Kubernetes
dependency set. Scope and clean test resources; avoid assertions based on namespace
garbage collection in envtest. Test that no-op reconciliation avoids status/write
loops and that Secret dependency changes work without spec generation changes.

## Research harness

Compare static gateway, lowest-latency, and adaptive strategies against the same
endpoint inventory, controlled workload, destination profiles, and failure schedule.
Include complete outage, latency degradation, jitter, destination-selective failure,
flapping, correlated provider/ASN failure, and recovery. Separate warm-up from
measurement and record the initial incumbent/history.

For each run retain a sanitized manifest with code revision, algorithm/config
version, seeds, clock model, engine versions/builds, fixture/data revision, workload
rate/concurrency, scenario timeline, probe budget, vantage/environment, and metric
definitions. Use multiple repetitions/seeds and uncertainty estimates; do not pick
only favorable traces. Export bounded sanitized observations and decisions without
exporting real secrets. Define retention separately for reproducible datasets.

Measure request success/availability, p95/p99 latency, time from fault to detection,
decision, publication, activation (where known), and recovered successful traffic;
count switches and explicitly define which were unnecessary. Include request loss
and connection disruption during reload. Equalize probe budgets across strategies
or report the difference. A trace replay isolates decision quality; a live controlled
traffic experiment measures system effects. Do both before claiming superiority.

The harness must call production policy/selection through ordinary boundaries,
without turning the runtime into a thesis simulator. No experimental improvement
or availability claim exists at bootstrap.
