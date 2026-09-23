# ADR 0018: Resilient managed HTTP sources

Date: 2026-09-23. Status: accepted. Resolves Q14.

## Context

M2 admits bounded source snapshots but the M6/M7 operator reads subscription bytes
only from Secrets. HTTP acquisition already enforces destination and redirect policy.
M8 must refresh HTTP subscriptions without making a failed fetch authoritative,
without exposing credentials, and without coupling source freshness to runtime LKG.

## Decision

Each ProxyPool source keeps its stable, safe `id`, explicit format and admission
flags. Its input is exactly one of the existing `secretRef` or a new `http` object.
The HTTP object names a same-namespace Secret key containing the complete URL and
optionally a Secret key containing an Authorization header value. It has separate
`allowHTTP`, `allowPrivateNetworks` and `allowInsecureTLS` opt-ins. TLS verification
remains enabled by default; only an explicitly trusted source may disable it. The
existing pool refresh interval owns periodic scheduling;
HTTP cache fallback has a separately bounded `maxStale` duration (default 24 hours,
range one minute to seven days). Secret sources retain their current behavior.

The logical source key is Pool UID plus source ID. A private SHA-256 compatibility
fingerprint covers the source variant, URL bytes, authorization bytes, format,
`allowEmpty`, `allowPartial`, pool `allowInsecureTLS`, and HTTP network/TLS flags.
It excludes Secret resourceVersion, names, and unrelated metadata: equivalent
material can safely reuse a cache after a metadata-only change. The fingerprint
stays in protected storage, never Kubernetes metadata/status. A changed fingerprint
has no eligible cache or validators. The current Secret object versions are still
captured for stale-input guards before publication.

The protected SQLite store holds at most one accepted bounded body per logical
HTTP source, its compatibility fingerprint, body SHA-256, content type, ETag,
Last-Modified, accepted time and last successful validation time. An explicit
schema migration adds this table. Writes are atomic. Cache reads verify schema,
identity, body length and digest, and parse/admit the bytes again through M2.
Missing, corrupt, incompatible or expired bytes cannot contribute to inventory.
Removed pools/sources and replaced identities are pruned; any unvalidated record
older than eight days is also removed. No historical bodies are retained. SQLite's
protected directory/file permissions and PVC remain required.

A 200 response is parsed and admitted before a cache transaction commits it. A
successful 304 only renews validation time and any validators explicitly returned
for a matching, intact, still eligible cached body. It never supplies empty bytes.
ETag and Last-Modified are stored as
provider validators, never content identity; missing validators are removed on a
new 200. A 304 without usable cache fails safely and gets at most one unconditional
request. A 200 with equivalent admitted inventory does not cause artifact rollout.

Fallback uses the last successful remote validation time. A failed acquisition or
admission retains a compatible cache only while age is strictly less than
`maxStale`; at the exact boundary it expires. Startup follows the same rule. On
expiry, the source contributes no current inventory and status reports expiry,
while artifact/runtime LKG remains governed by its own rules. Successful 304
renews validation time, not accepted body time.

Network timeouts, temporary connection failures, 429 and 5xx may retry within
three attempts and an overall bounded deadline. Parse/admission, configuration,
SSRF denial and other 4xx failures do not retry. Backoff is exponentially bounded
with jitter; a bounded Retry-After may delay a retry but cannot extend the total
deadline. Cancellation interrupts request/backoff and leadership loss cancels work.
Refresh jobs are bounded globally and per source, spread deterministically after
restart, and run outside ordinary API reconcile workers. Pool status exposes only
source ID, safe state/reason and timestamps; no URL, validator, digest or raw error.

## Alternatives

Persisting snapshots as Kubernetes Secrets would add sensitive API objects and
large write/watch churn. Keeping cache only in memory cannot survive restart.
Using ETag as integrity or compatibility identity trusts provider-controlled data.
Resetting cache on every Secret resourceVersion change discards valid content after
metadata-only changes. Unlimited fallback hides permanently stale inventory.
An additional database or a Kubernetes-dependent source core would duplicate
existing boundaries.

## Consequences

The operator's protected persistent volume is necessary for restart recovery.
Cache freshness is distinct from endpoint evidence, validated artifact and runtime
readiness. Existing Secret source objects remain valid with no migration. Changing
source kind or credential/configuration semantics requires a new accepted HTTP
revision before HTTP content is again eligible.
