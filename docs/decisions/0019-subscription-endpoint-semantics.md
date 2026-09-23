# ADR 0019: Bounded subscription endpoint semantics

Date: 2026-09-24. Status: accepted.

## Context

M2's URI parser and M3's renderers admit only VLESS/Trojan with TCP or WebSocket
path and ordinary TLS. Real subscriptions also contain Base64 VMess JSON shares,
SIP002 Shadowsocks shares and full Xray/V2Ray client configurations. Treating a
parsed URI or JSON record as a working endpoint before both pinned renderers can
express it would violate the validated-artifact boundary.

## Decision

The supported extension is VMess UUID with alter ID zero, explicit supported
security method, TCP or WebSocket, optional ordinary TLS; and plugin-free
Shadowsocks AEAD (`aes-128-gcm`, `aes-256-gcm`, or
`chacha20-ietf-poly1305`) over TCP. WebSocket Host is represented separately from
TLS SNI. VLESS/Trojan retain their existing semantics and may use WebSocket Host.
Unknown connection-critical options are classified unsupported, not discarded.
Unsupported records remain subject to the existing fail-closed admission default.

The existing version-one endpoint IDs and private revisions remain byte-for-byte
stable for old configurations. New protocols or WebSocket Host use version-two
canonical identity domains, encoding method and Host after the version-one fields.
Credentials remain only in the private connection revision. Equivalent URI and
supported JSON records construct the same domain model and identity. `ef2_` IDs
are accepted by state restoration alongside `ef1_`.

JSON detection is structural and ignores Content-Type. A recognized JSON body is
never retried as a URI list after a decoding or admission error. Explicit JSON
supports URI arrays, objects with a `nodes` or `proxies` URI array, and Xray/V2Ray
complete configuration objects or arrays thereof. Xray extraction reads only
`outbounds`; routing, DNS, inbounds and UI metadata have no authority here.
The same bounded rule accepts sing-box typed outbound arrays or full configuration
objects containing typed `outbounds`, without importing its route or inbounds.
Freedom/direct, blackhole/block and other service entries are not proxy endpoints.
Supported proxy outbounds map through the same endpoint constructors. Nested
`settings.vnext[].users[]` and `settings.servers[]` are bounded and expanded rather
than selecting only one item. Repeated profiles deduplicate normally. Reality,
Vision flow, client fingerprints, ALPN, plugins and transports beyond TCP/WS are
explicitly unsupported until end-to-end implementation. A document containing
only unsupported proxy entries fails the normal non-empty admission boundary.

## Alternatives

Generic native JSON/YAML import would give source data control over routing and
runtime output; it is rejected. Broad URI acceptance with dropped fields would
construct broken connections; it is rejected. Persisting random HWIDs would add
state and migration complexity without a compatibility benefit for this bounded
source identity contract.

## Consequences

The format matrix remains deliberately narrow and engine/version-specific. Native
validation and controlled traffic tests must qualify newly claimed protocol
support before completion. Reality requires a separate pre-v1.0 decision covering
typed key/short ID/fingerprint/flow parameters, both renderers, probes and native
traffic, rather than a superficial parser path. The full accepted/unsupported
matrix lives in the [source design](../designs/endpoints-and-sources.md).
