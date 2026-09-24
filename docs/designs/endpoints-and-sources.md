# Endpoints and sources

Status: implemented M1 identity contract; M2 source pipeline contract accepted.
This document owns normalization and acquisition semantics. The durable identity
boundary is recorded in [ADR 0006](../decisions/0006-versioned-endpoint-identity.md).
The bounded M8 compatibility extension is specified in the
[subscription matrix](subscription-compatibility.md) and
[ADR 0019](../decisions/0019-subscription-endpoint-semantics.md).
The planned [M8.5 milestone](../roadmap/p1.md#m85--protocol-and-transport-compatibility)
expands connection semantics only after C1 resolves the exact engine capability
and identity gates; this document does not claim those combinations work today.

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
- VLESS UUID hex is case-insensitive and stored in lower-case hyphenated form; the
  nil UUID is rejected. Trojan passwords are 1–4096 bytes of valid UTF-8. Passwords
  are byte-for-byte significant and are not trimmed or case-folded.
- WebSocket paths are 1–2048 bytes of valid UTF-8, contain no control characters,
  begin with `/`, and otherwise remain byte-for-byte significant. M1 does not infer
  an engine-specific default path.
- An omitted TLS server name becomes the canonical endpoint host. Explicit DNS
  server names use the same DNS case/root-dot normalization; IP text is canonicalized.
  TLS-disabled configuration cannot carry TLS options.
- Source IDs and source-local record IDs are 1–128 ASCII letters, digits, dots,
  underscores, or hyphens. They are safe application identifiers, not raw source
  URLs. Each alias is 1–256 bytes of valid UTF-8 without control/format characters;
  aliases are never formatted by domain summary methods and never affect identity.

Canonical encodings use a four-byte big-endian length before every UTF-8 string,
two-byte big-endian ports, one byte for enums, and `0`/`1` bytes for booleans. The
field order is domain string, protocol, host kind, host, port, transport, WebSocket
path, TLS enabled, TLS server name, and insecure-verification flag. Version 1 enum
codes are VLESS `1`, Trojan `2`; DNS `1`, IPv4 `2`, IPv6 `3`; TCP `1`, WebSocket `2`.

The logical domain is `egressfox.endpoint/v1`. Its SHA-256 digest is lower-case
unpadded Base32 with prefix `ef1_`. The private connection encoding uses domain
`egressfox.connection/v1`, followed by the same fields and then credential protocol
and canonical credential string; its SHA-256 digest is kept private. A
canonicalization change that can alter equivalence requires a new version and
migration. Implementations must not reinterpret stored version 1 IDs under new rules.

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

### M2 source and format slice

[ADR 0007](../decisions/0007-safe-source-snapshots.md) owns the durable source
snapshot decision. M2 implements two acquisition paths: caller-supplied bounded
bytes and HTTP/HTTPS. Both produce the same payload boundary before format handling;
a future file or Secret adapter can use it without changing parsers. The stable
source identity is a caller-assigned safe `SourceID`. Locations, query strings and
headers are secret-bearing configuration and never become identity or provenance.

HTTP uses HTTPS by default. Plain HTTP, ordinary private destinations and
loopback require separate explicit options. Link-local, metadata, multicast and
unspecified destinations remain prohibited even with private-network permission.
Resolution policy applies to actual dial addresses,
including IP literals and IPv4-mapped IPv6, and to every redirect. Redirects are
bounded and same-origin. M2 neither requests nor decodes compressed content. Retry,
cache validators, persistent content cache and cross-origin credential forwarding
are unsupported. These controls reduce source-fetch SSRF exposure; they do not
authorize endpoints returned by the source.

M2 recognizes plain line-oriented URI lists and a single standard or raw-standard
Base64 envelope around a URI list. URL-safe Base64 and recursive envelopes are not
accepted. Explicit format selection is preferred. Auto-detection first recognizes
known proxy URI schemes and otherwise chooses Base64 only after strict decoding to
a recognizable URI list. Blank lines and lines beginning with `#` are ignored. The
default limits are 4 MiB input, 4 MiB decoded input, 10,000 records, and 16 KiB per
record. Hard configurable ceilings are 64 MiB input/decoded bytes, 1,000,000 records,
and 1 MiB per record.

The URI subset maps exactly to M1. VLESS requires UUID authentication, an explicit
port, `encryption=none`, empty flow, TCP or WebSocket with an explicit path, and
`security=none|tls`. Trojan requires password authentication, an explicit port,
TLS, and TCP or WebSocket with an explicit path. Both may carry SNI and an explicit
certificate verification flag. URI fragments become source-local alias metadata
only after M1 validation. Duplicate query keys are malformed. Unknown query keys
and recognized connection features outside M1 are unsupported because ignoring
them could change connectivity. Other known proxy schemes are unsupported records;
arbitrary non-URI text is malformed.

Diagnostics classify records as malformed, unsupported or invalid and expose only
source ID, stable ordinal, bounded reason code and category. Counts form the M2
source-metrics boundary. Raw lines, URIs, fragments, locations, headers, credentials,
query values and parser error text are never retained in diagnostics.

## Refresh semantics

Each attempt has a timeout, cancellation, byte and redirect limits, and sanitized
errors. M8 adds Secret-backed Authorization, conditional ETag/Last-Modified requests,
bounded retry/backoff and protected source-cache fallback for Kubernetes HTTP
sources. Arbitrary custom headers and User-Agent configuration remain future work.
Source URL, authorization and validators are secret-bearing; diagnostics identify a
configured source by safe ID, never by URL or query string.

| Refresh result | Inventory consequence |
| --- | --- |
| Valid complete snapshot | Commit a new source revision, then reconcile source-local presence |
| Explicit valid empty snapshot | A deliberate inventory change, subject to configured empty-source safety policy |
| HTTP 304 with matching usable cache (M8) | Reuse that revision and renew validation time; not an empty response |
| Timeout, authentication failure, oversized or malformed response | Failed attempt; do not erase previous inventory |
| Mixed valid and rejected records | Reject transactionally by default; explicit partial policy may commit accepted records with bounded diagnostics |
| Source removed from desired configuration | Explicit removal; not equivalent to a transient refresh error |

The M8 operator retains a compatible accepted HTTP source body during a failed
attempt only until its `maxStale` boundary. The default is 24 hours; the API permits
one minute to seven days. At the exact expiry boundary the source no longer
contributes current inventory. Source content, endpoint evidence, published
configuration and managed runtime each have separate last-known-good rules.

### M8 managed HTTP refresh

M8 exposes the existing HTTP acquisition path through the validated Secret/HTTP
source union. The URL and optional Authorization value are same-namespace Secret
keys. The pool refresh interval schedules a leader-scoped, four-worker queue outside
API reconcile workers. Three attempts within a 45-second overall deadline retry
only temporary network/timeout, 429 and 5xx failures. Backoff starts at 200 ms,
adds bounded jitter and honors Retry-After up to two seconds. A canceled leader
context interrupts requests and backoff. A 304 can reuse only a matching, intact,
unexpired cache; a 304 without it gets at most one unconditional request.

[ADR 0018](../decisions/0018-resilient-http-sources.md) defines private compatibility
fingerprinting and SQLite schema v3. The source key is Pool UID plus source ID;
URL bytes, Authorization bytes, format/admission and HTTP authorization affect
compatibility. Secret metadata alone does not. The cache stores the bounded body,
body digest, validators, accepted time and last successful validation time. Reopen
verifies integrity and re-parses through M2. A failed fetch or parse never commits
new bytes. Removed/incompatible source records are pruned, and unvalidated records
older than eight days are removed. Raw cache bytes and fingerprints never
enter CR status or metadata.
The durable client identity reference in [ADR 0020](../decisions/0020-subscription-identity-and-source-destination-policy.md)
controls the automatic HWID independently of source provenance and cache identity.
The legacy default preserves existing HWIDs. Explicit identity continuity survives
source renames or moves; request/format changes still invalidate incompatible cache
bytes and validators without rotating HWID.

File, environment, ConfigMap and Vault adapters are not in the committed P1 path.
Subscription quota/expiry remains optional attributed metadata, never trusted
selection policy. The bounded structured subscription formats already admitted
by M8 remain governed by the [compatibility matrix](subscription-compatibility.md).
M8.5 protocol/transport expansion requires C1 semantic and engine review; it
does not authorize native configuration composition.

An M2 committed snapshot is the complete current contribution of one source. A
successful replacement removes relationships absent from that source while an
endpoint contributed by another committed source remains. A failed attempt changes
nothing. A non-empty input with no accepted records always fails. Explicit valid
empty input is distinct from failure and commits only under an allow-empty policy;
source removal is a separate deliberate operation. Since sharing links have no
stable provider record key, their safe source-local record ID is the M1 logical
endpoint ID. The confidential full identity still distinguishes credential
revisions, so rotation retains record continuity without reusing current health.

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
[ADR 0006](../decisions/0006-versioned-endpoint-identity.md), and Q2 by
[ADR 0007](../decisions/0007-safe-source-snapshots.md). M8 implemented protected
persistent source caching and bounded JSON extraction. Additional connection
combinations belong to M8.5 after C1; arbitrary native configuration import
remains outside this milestone.
