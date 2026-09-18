# Endpoints and sources

Status: accepted M1 identity contract; source acquisition remains design direction.
This document owns normalization and acquisition semantics. The durable identity
boundary is recorded in [ADR 0006](../decisions/0006-versioned-endpoint-identity.md).

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

### M1 supported semantic slice

M1 admits only the intersection needed to establish the domain contract:

| Field | Supported behavior |
| --- | --- |
| Protocol | VLESS with empty flow; Trojan |
| Server | DNS hostname, IPv4, or IPv6 plus port 1–65535 |
| Credential | VLESS UUID; Trojan password |
| Transport | Direct TCP; WebSocket with an explicit request path |
| TLS | Optional for VLESS and required for Trojan; effective server name and certificate-verification mode |

This is a normalization and inventory claim, not a renderer support claim. VLESS
flow, UDP/packet modes, WebSocket headers and early data, gRPC/HTTP/QUIC transports,
ALPN, Reality, ECH, certificate pins, client fingerprints, mTLS, multiplexing, and
dial options are outside M1. Source adapters must reject an input that uses an
unsupported connectivity field rather than discard the field. The
[sing-box outbound](https://sing-box.sagernet.org/configuration/outbound/) and
[Mihomo outbound](https://wiki.metacubex.one/en/config/proxies/) documentation was
reviewed on 2026-09-18 to choose this common subset; target/version support remains
an M3 renderer decision.

### Canonicalization and identity version 1

M1 has two related identity layers:

- The **logical endpoint ID** includes protocol, normalized server and port,
  transport kind and WebSocket path, TLS enabled state, effective TLS server name,
  and certificate-verification mode. It excludes credentials, aliases, provenance,
  provider assertions, observations, and lifecycle timestamps.
- The **connection revision** includes the same fields and canonical credential
  material. The full connection identity is logical ID plus revision. The revision
  is confidential: it is comparable inside the domain but has no public text,
  byte, JSON, log, metric, status, or error representation.

A credential rotation is the same logical endpoint with a new connection revision.
Current observations must use the full identity and cannot cross that boundary.
Two simultaneously discovered credentials for one logical endpoint are distinct
inventory records. Historical analysis may link them by logical ID only when it
explicitly accounts for the revision change.

Version 1 applies these canonicalization rules:

- DNS hostnames are ASCII, case-insensitive, and stored lower-case without one
  terminal root dot. Empty labels, invalid label edges, non-ASCII input, and values
  longer than DNS limits are rejected. Internationalized names must arrive as
  explicit ASCII A-labels; M1 does not perform implicit IDNA conversion.
- IPv4 and IPv6 use Go `net/netip` canonical text. Brackets around an IPv6 host are
  accepted and removed. IPv6 zones are rejected. IPv4-mapped IPv6 stays IPv6 and is
  not silently collapsed to IPv4.
- VLESS UUID hex is case-insensitive and stored in lower-case hyphenated form.
  Trojan passwords and WebSocket paths are byte-for-byte significant after UTF-8
  validation; they are not trimmed or case-folded. A WebSocket path must be explicit
  and begin with `/`.
- An omitted TLS server name becomes the canonical endpoint host. Explicit DNS
  server names use the same DNS case/root-dot normalization; IP text is canonicalized.
  TLS-disabled configuration cannot carry TLS options.
- Source IDs and source-local record IDs are safe application identifiers. Aliases
  are bounded, valid UTF-8 display data; they are never formatted by domain summary
  methods and never affect identity.

Canonical encodings begin with distinct `egressfox.endpoint/v1` and
`egressfox.connection/v1` domains. Fields are written in the order above as
length-prefixed UTF-8 or fixed-width scalar values, so concatenation is unambiguous.
The logical SHA-256 digest is lower-case unpadded Base32 with prefix `ef1_`.
The connection digest is kept private. A canonicalization change that can alter
equivalence requires a new version and migration; implementations must not reinterpret
stored version 1 IDs under new rules.

### Identity acceptance properties

M1 must settle and test a canonicalization contract before persistence depends on it:

- Equivalent supported input representations produce the same identity across runs.
- Renaming/reordering a source or endpoint preserves connection identity.
- Distinct authentication changes the private connection revision. Distinct TLS,
  transport, address, port, or protocol semantics change the logical ID.
- Normalize DNS names, IP representations, defaults, and absent-versus-empty fields
  only where equivalence is established. Do not lowercase case-sensitive paths,
  arbitrary credentials, or SNI-related values without a defined rule.
- Keep stable hostnames in connection identity; a DNS answer changing is an observation,
  not necessarily a new endpoint. Explicit IP endpoints remain distinct.
- A version prefix and migration policy permit canonicalization changes without
  silently joining old measurements to unrelated endpoints.
- Unrecognized fields affecting connectivity are rejected or retained as explicitly
  unsupported input; dropping them must not falsely merge two configurations.
- Resolved credential material participates only in the private connection revision.
  A Secret reference name or API-server resourceVersion is not semantic identity.
  Adapters must resolve credentials before constructing a connection; historical
  continuity uses the logical ID while current health remains revision-specific.

The connection revision is **sensitive**: its plain digest does not protect
low-entropy secrets. M1 exposes equality but not its bytes. Future protected
persistence must be designed with the history schema; the revision must not appear
in metric labels, CR status, errors, or diagnostics. The logical ID is safe for
bounded diagnostics but remains opaque and is not a secrecy guarantee. The future
`explain` UI may show it with aliases that are not assumed unique.

### Deduplication and provenance

M1 deduplicates within one caller-provided inventory/trust scope using the full
connection identity. It verifies complete configuration equality after an identity
match; a mismatch is a deterministic conflict rather than an arrival-order choice.
Records are ordered by logical ID and then private revision.

Provenance is an inventory relationship, not part of endpoint identity. Each
association has a safe source ID, a safe source-local record ID, and zero or more
untrusted display aliases. Duplicate associations union and sort aliases; endpoint
records union and sort associations. No single display name is selected. Two sources
contributing the same connection do not make two endpoints or independent failure
domains. Removing one association later must not remove a record that retains
another association. Different connection revisions at the same logical endpoint
remain separate records and therefore cannot share current probe health.

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
cross-tenant inventory sharing, or universal schema is proposed. Q1 is resolved by
[ADR 0006](../decisions/0006-versioned-endpoint-identity.md). Q2 in the
[decision queue](../decisions/open-questions.md) covers source admission, partial
acceptance, and the first input format. M1's semantic slice does not choose that
format or begin acquisition.
