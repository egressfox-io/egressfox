# ADR 0023: Bounded Hysteria2 and QUIC network boundary

Date: 2026-09-25. Status: accepted for C4 implementation; native and managed
traffic qualification remains pending in [plan 0020](../plans/0020-m85-protocol-transport-compatibility.md).

## Context

[ADR 0021](0021-version-three-connection-semantics-and-capabilities.md) reserved
Hysteria2 domain fields and ef3 identity. The pinned sing-box source registers
Hysteria2 only with `with_quic`; C3's `with_utls` must remain. A hostname checked
by EgressFox does not constrain an engine's later DNS lookup unless the engine
receives the authorized literal. Both pinned renderers accept literal servers
separately from TLS server names. Their port hopping uses the same server host.

## Decision

- Keep the pinned engine source commits. Change sing-box build revision to 3 with
  `with_quic,with_utls` and evidence profile `egressfox.sing-box/v3`.
- Admit password, ordinary TLS verification/SNI, ordered ALPN, paired Mbps,
  optional Salamander secret and at most 16 explicit or ranged port entries
  covering at most 256 UDP ports. A hop interval is the engine default. Reject
  realm, Gecko, tuning, certificate pinning and unknown connection fields.
- Extend ef3 encoding only for nonempty port sets. Existing ef1/ef2/ef3 canonical
  bytes stay unchanged. Authentication and obfuscation secrets remain in the
  private revision; the logical ID carries only obfuscation presence.
- Before a probe, authorize every DNS answer and replace the engine server host
  with one allowed literal. Retain TLS SNI, credentials, ALPN, obfuscation and
  the entire bounded port set. Every hop is therefore constrained to that same
  authorized address. Target authorization remains separate.

## Alternatives

Engine-side hostname resolution would permit rebinding after authorization.
Selecting only the first hop would change connection semantics. Expanding
unbounded ranges would exhaust work and broaden egress. Custom QUIC dial code
would cross the control-plane/dataplane boundary.

## Consequences

The new sing-box profile cannot reuse observations from revision 2. Existing
endpoint IDs and revisions retain their bytes. The bounded subset still needs
exact native checks, controlled QUIC probes and managed traffic on both engines
before C4 can be declared qualified.
