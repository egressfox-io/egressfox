# Endpoints and sources

Status: design direction; no source adapter, parser, or endpoint type exists yet.
This document owns normalization and acquisition semantics. Exact Go structs,
serialization, and identity encoding remain design work for M1.

## Decisions and requirements

Endpoint identity is deterministic and independent of provider display names,
source ordering, observation time, score, and mutable geography labels.
Deduplication must preserve all provenance. A changed credential or TLS/transport
setting must not accidentally reuse health measured for a different connection.

An endpoint describes a connection that an engine can implement. EgressFox does
not implement that protocol. Protocol support is explicit per input format and
engine/version; recognizing a protocol name is not support for all its variants.

## Model direction

Keep these concepts distinguishable, whether or not they become separate Go types:

| Concept | Contents and reason |
| --- | --- |
| Connection semantics | Protocol, host/address, port, protocol parameters, transport, TLS and authentication material/reference revision |
| Identity and revision | Versioned canonical identity; enough change information to invalidate incompatible observations |
| Descriptive metadata | Display aliases, tags, source assertions about country/ASN; excluded from connection identity |
| Provenance | Stable source ID and source-local presence, first/last seen, parse origin without storing raw URIs in diagnostics |
| Observed metadata | Exit IP, measured country/ASN, vantage and timestamp; not blindly merged with provider assertions |
| Lifecycle | First seen, last seen, removed/stale state with retention policy |

Expected ecosystems include VLESS, VMess, Trojan, Shadowsocks, Hysteria/Hysteria2,
TUIC, SOCKS, HTTP proxies, and WireGuard where the engine model fits. This is an
evolution list, not a commitment to implement them all before the first useful slice.
TLS includes server name, verification behavior, and applicable client parameters;
transport includes protocol-specific options such as WebSocket paths or service names.
An untyped bag of strings shared by all renderers would hide unsupported semantics.
Typed protocol variants should follow actual parsing/rendering needs.

### Identity acceptance properties

M1 must settle and test a canonicalization contract before persistence depends on it:

- Equivalent supported input representations produce the same identity across runs.
- Renaming/reordering a source or endpoint preserves connection identity.
- Distinct authentication, TLS identity, transport, or protocol semantics remain distinct.
- Normalize DNS names, IP representations, defaults, and absent-versus-empty fields
  only where equivalence is established. Do not lowercase case-sensitive paths,
  arbitrary credentials, or SNI-related values without a defined rule.
- Keep stable hostnames in connection identity; a DNS answer changing is an observation,
  not necessarily a new endpoint. Explicit IP endpoints remain distinct.
- A version prefix and migration policy permit canonicalization changes without
  silently joining old measurements to unrelated endpoints.
- Unrecognized fields affecting connectivity are rejected or retained as explicitly
  unsupported input; dropping them must not falsely merge two configurations.
- Decide how resolved Secret revisions participate. Reference name alone cannot
  distinguish credential rotation, and API-server resourceVersion is not a portable
  semantic identity. Historical continuity after rotation requires an explicit rule.

An identity digest involving authentication material is **sensitive**: a plain
hash does not protect low-entropy secrets. The choice between a private canonical
digest, keyed fingerprints with key lifecycle, and separate stable record/connection
revisions is open (Q1). Do not publish such hashes as metric labels or CR status.
The future `explain` UI needs safe opaque IDs and aliases that are not assumed unique.

### Deduplication and provenance

Deduplicate within an explicit inventory/credential trust scope. Two sources
contributing the same endpoint do not make two independent failure domains.
Union provenance, preserve conflicting assertions with their origin, and define
a deterministic display-name choice separately. Do not overwrite metadata based
on fetch order. Removing a source association must not remove an endpoint still
present in another source.

Failure-domain source limits require a documented attribution rule for multi-source
endpoints. Provider, subscription, ASN, and physical gateway are not synonyms.
Unknown country/ASN must have defined treatment instead of being counted as unique
diversity. These constraints are developed in the selection design at P1.

## Acquisition boundary

Acquisition obtains bounded bytes and source revision metadata. Parsing interprets
an explicit or safely detected format. Normalization produces validated connection
semantics. Admission applies static filters before expensive probing. None of these
steps should mutate a live output configuration.

Planned source adapters include HTTP/HTTPS, local files, inline input, environment
references, Kubernetes Secrets, and Vault. Start with the subset in the roadmap;
Secret/Vault resolution belongs at adapters, not in the domain model. Environment
variables are a deployment option, not evidence of secure secret storage.

Planned format families are URI lists, Base64 subscription envelopes, Mihomo/Clash
YAML, and sing-box JSON. Base64 is an encoding, not encryption or authentication.
Bound both encoded and decoded sizes, nesting, record count, and per-record fields.
Disallow recursive autodetection, YAML alias expansion bombs, duplicate-key ambiguity,
and remote includes unless a later explicit, bounded feature supports them.

Engine configs used as sources supply endpoint descriptions only. Do not import
their listeners, DNS configuration, routing, executable hooks, file paths, or runtime
API settings into desired output. Native escape hatches are trusted user intent in
a separate renderer boundary; subscriptions cannot inject them.

## Refresh semantics

Each attempt has a timeout, cancellation, byte and redirect limits, and sanitized
errors. HTTP authentication, custom headers, User-Agent, retries/backoff, ETag,
Last-Modified, caching, source fallback, and last-known-good content are planned
capabilities. Source URL and headers are secret-bearing; logs should identify a
configured source by safe ID, not its URL or query string.

| Refresh result | Inventory consequence |
| --- | --- |
| Valid complete snapshot | Commit a new source revision, then reconcile source-local presence |
| Explicit valid empty snapshot | A deliberate inventory change, subject to configured empty-source safety policy |
| HTTP 304 with matching usable cache (P1) | Reuse that revision; not an empty response |
| Timeout, authentication failure, oversized or malformed response | Failed attempt; do not erase previous inventory |
| Mixed valid and invalid records | Default proposed behavior is reject the snapshot; any partial acceptance needs opt-in semantics and bounded rejection diagnostics |
| Source removed from desired configuration | Explicit removal; not equivalent to a transient refresh error |

P0 requires transactional refresh and failure reporting. Durable HTTP content
caching/fallback is P1; retaining the currently committed inventory during a failed
attempt does not promise a complete source-cache implementation. Staleness/expiry
must be visible. Selection must not keep an endpoint indefinitely simply because
its source failed to refresh. Last-known-good *source content* and last-known-good
*published configuration* are separate objects and policies.

## Security and test obligations

Apply the [threat model](../security/threat-model.md) to source hosts, endpoint
addresses, redirects, and probe destinations. Allowing a subscription fetch must
not implicitly authorize connections to every address it returns. Resolve and check
network policy at connection time; account for DNS rebinding and IPv4-mapped IPv6.
Never forward authorization headers to a redirected origin by default.

Use synthetic fixtures, table tests for equivalence/non-equivalence, permutation
tests for stable deduplication, and parser fuzzing with size/depth limits. Test
source failures independently from authoritative emptiness and source removal.
Tests must not contact real subscription providers.

## Non-goals and open questions

No proxy implementation, lossless editor for arbitrary engine configuration,
cross-tenant inventory sharing, or universal schema is proposed. Q1 in the
[decision queue](../decisions/open-questions.md) covers identity secrecy, rotation,
and canonicalization. Q2 covers source admission/partial acceptance and the initial
protocol/format slice. Do not choose a struct merely to make this document concrete.
