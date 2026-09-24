# ADR 0021: Version-three connection semantics and exact-profile capability gate

Date: 2026-09-24. Status: accepted for M8.5 C1.

## Context

ADRs [0006](0006-versioned-endpoint-identity.md) and
[0019](0019-subscription-endpoint-semantics.md) define the shipped `ef1_`/`ef2_`
connection encodings. M8.5 needs more protocols and security/transport variants
without reinterpreting stored observations or allowing a renderer to omit a field.
The [versioned matrix](../designs/engine-protocol-compatibility.md) records the
pinned-engine research. Upstream capability and EgressFox traffic qualification
are separate facts.

## Decision

- Retain the existing `Configuration`, `Credential`, `Transport` and `TLSConfig`
  entry points and **their exact v1/v2 canonical bytes**. Add typed, validated
  extensions on the same canonical configuration. No native JSON/YAML or
  arbitrary map becomes domain data. HTTP and HTTPS CONNECT are one HTTP proxy
  protocol with TLS disabled/enabled; SOCKS5 is distinct. Hysteria2 has its own
  required QUIC transport and TLS. VMess payload security and Shadowsocks cipher
  remain protocol fields, distinct from outer TLS. Reality, ordered ALPN and
  client fingerprint are security fields; Vision is a VLESS flow.
- Extended configurations use domain-separated `ef3_` logical IDs and private
  revisions. The v3 encoding is length-delimited and includes connection-critical
  protocol, destination, transport, flow, TLS, ALPN, fingerprint, Reality public
  key and Hysteria2 bandwidth/obfuscation presence. Credentials, proxy username,
  Reality short ID and Hysteria2 obfuscation password enter only the confidential
  revision. The short ID is treated as private because it can be low entropy.
  Ordered ALPN is not sorted; request paths and gRPC service names retain case.
  DNS names/IPs use the existing normalization, and UUIDs use the existing UUID
  normalization. No generic URI normalization is applied to opaque fields.
- A new configuration is **valid domain data** only if its protocol, transport,
  TLS/security and typed options are consistent. The current narrow constructors
  remain unchanged. HTTP/2 without TLS is rejected because sing-box's `http`
  transport would use HTTP/1.1. Reality is initially restricted to VLESS over
  direct TCP, and Vision requires TLS on direct TCP. Hysteria2 requires QUIC/TLS.
  XHTTP has a reserved discriminator but no constructible C1 value: mode, headers,
  padding and engine-specific behavior must be specified and qualified in C3.
  Additional advanced parameters are rejected by ingestion until their slices
  give them typed semantics. Valid but unimplemented configurations may exist in
  inventory; they are not accepted by source parsers merely because the type exists.
- `engine.CheckEndpoint` is the exact-profile EgressFox capability gate used by
  both renderers and the probe executor. It admits only the previously qualified
  VLESS/Trojan/VMess/Shadowsocks TCP/WS and ordinary TLS subset on the pinned
  profiles. A valid newly modeled form returns a safe `ErrUnsupported` with a
  field/feature code, before rendering or probing. Invalid domain data returns
  `endpoint.ErrInvalid`. Neither path produces a negative health observation.
  Each C2–C4 support expansion must update this gate, renderer and through-engine
  evidence together, with engine-specific decisions where appropriate.
- Probe execution may substitute an authorized literal destination through
  `WithAddress` while retaining all other connection options and the original
  observation identity. No SQL or CRD schema changes are needed. `ParseID`
  restores `ef3_` alongside the older prefixes; the complete ID/private-revision
  pair prevents incompatible v3 evidence from joining v1/v2 evidence or a
  different credential revision. Existing SQLite rows and selection checkpoints
  remain valid and untouched. Unsupported records must be excluded from future
  engine-specific probe/selection eligibility; an execution error is not health.
- C4 must authorize every UDP/QUIC endpoint address and any port-hopping/realm
  destination, recheck DNS answers, and attribute through-engine probes to the
  original revision. C1 adds no UDP dial path or policy exception. Port hopping,
  realm, QUIC tuning and extra obfuscators await C4's typed contract and tests.

## Alternatives

Changing v1/v2 serialization would churn public IDs and risk observation reuse.
Generic option maps would hide unvalidated fields. Encoding every new variant as
ordinary TLS/TCP would silently weaken the connection. Adding a second endpoint
model would split source, probe and renderer semantics. Treating an unsupported
probe as a failed connection would poison adaptive selection.

## Consequences

The existing IDs, revisions, rendered bytes, SQLite schema and source cache are
unchanged. A new typed configuration receives an independent v3 identity and
cannot become a published artifact until its engine-specific slice qualifies it.
The matrix owns combination status; this ADR owns identity and capability rules.
The remaining parameter-level XHTTP and Hysteria2 network decisions are explicit
C3/C4 gates rather than guessed C1 public semantics.
