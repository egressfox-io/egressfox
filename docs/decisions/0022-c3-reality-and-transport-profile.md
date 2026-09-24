# ADR 0022: Bounded C3 Reality and transport profile

Date: 2026-09-24. Status: accepted for M8.5 C3 implementation; managed-path
qualification remains recorded in [plan 0020](../plans/0020-m85-protocol-transport-compatibility.md).

## Context

[ADR 0021](0021-version-three-connection-semantics-and-capabilities.md) defined
typed advanced fields and fail-closed exact-profile gating, but did not enable
their rendering. The pinned sing-box source requires `with_utls` for Reality.
The pinned Mihomo source has a VLESS XHTTP implementation, while sing-box 1.14.1
has no XHTTP transport. Upstream names also distinguish a TLS certificate
fingerprint from a uTLS client fingerprint.

## Decision

- Retain the pinned Mihomo and sing-box source commits and Go version. Raise the
  sing-box derivative build to revision 2 with `with_utls`, and change its exact
  profile from `egressfox.sing-box/v1` to `/v2`. The release manifest, Dockerfile,
  notices and corresponding-source naming agree. `with_quic` remains a C4 gate.
  An observation associated with the old engine profile cannot be reused by the
  new profile.
- Admit VLESS over TCP with Reality and Vision, explicit SNI, public
  key, short ID, ordered ALPN and client fingerprint. `xtls-rprx-vision` is the
  only admitted flow. Reality never becomes ordinary TLS. The common stable
  fingerprint subset includes `qq` and `360` because both pinned sources map
  those exact names; unknown names are invalid, never substituted. Dynamic and
  deprecated variants await a separate compatibility decision.
- Admit bounded VLESS HTTP/2, HTTPUpgrade and gRPC with typed path, Host or
  service name and ordinary TLS. Plain variants remain unqualified. Mihomo's explicit V2Ray HTTP upgrade mode is
  used for HTTPUpgrade. These are distinct transports in ef3 identity.
- Admit Mihomo VLESS XHTTP with ordinary TLS, explicit path/Host and one of
  `stream-one`, `stream-up` and `packet-up`. Sing-box rejects XHTTP before
  rendering or probing. Auto mode and additional request settings remain
  unsupported input until their connection semantics and network behavior are
  separately specified. The existing v3 service-string slot encodes XHTTP mode;
  all previously constructible ef3 byte sequences are unchanged.
- Preserve the original hostname and protocol-level SNI/Host during
  execution-only literal-IP substitution. A short-ID change rotates only the
  confidential revision; key, flow, mode and other public connection semantics
  change the logical ID. No SQL, CRD or public API migration is required.

## Alternatives

Upgrading sing-box to gain XHTTP would expand the version and release review
without a pinned implementation. Translating XHTTP into WS or Reality into TLS
would alter the requested wire protocol. Accepting arbitrary fingerprint names
would permit a client to fall back or fail only after publication. Appending a
new field to every v3 canonical record would unnecessarily change older ef3
identities.

## Consequences

The [engine matrix](../designs/engine-protocol-compatibility.md) lists exact
input, build and evidence stages. Every new claimed combination still requires
native validation, controlled through-engine traffic and managed Gateway traffic
before C3 is marked complete. Existing C1/C2 records and no-tag source-build
history remain historical facts; this decision changes only the new C3 profile.
