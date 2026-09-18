# ADR 0006: Versioned logical endpoint IDs and confidential connection revisions

Date: 2026-09-18. Status: accepted.

## Context

Endpoint identity must support deterministic deduplication, safe diagnostics, and
observation invalidation. Provider names and source metadata are unstable. Network
coordinates alone are insufficient because a credential, TLS, or transport change
can produce a materially different connection. Putting raw credentials or a plain
credential digest in a public ID would expose secrets or enable offline guessing of
low-entropy values.

M1 needs a real but deliberately small protocol slice. The common VLESS and Trojan
fields documented by [sing-box](https://sing-box.sagernet.org/configuration/outbound/)
and [Mihomo](https://wiki.metacubex.one/en/config/proxies/) were reviewed on
2026-09-18. Both engines describe server/port, authentication, TLS, and V2Ray-style
transport settings, but their full option sets and defaults are not interchangeable.

## Decision

Represent connection identity as two related values:

1. A versioned logical endpoint ID covers non-secret connection semantics: protocol,
   normalized server and port, the admitted transport and its options, and TLS
   behavior. It excludes credentials and all descriptive metadata. This ID is safe
   for bounded diagnostics, although it is not a secrecy boundary.
2. A confidential connection revision covers the complete admitted configuration,
   including canonical credential material. It is deterministic and comparable in
   memory, but its bytes and text are not exposed through formatting, errors, public
   serialization, metrics, or status. A plain digest is treated as sensitive rather
   than as credential protection.

The full connection identity is the pair of logical ID and confidential revision.
Observations must refer to that pair. Credential rotation therefore preserves the
logical ID but creates a new connection revision, so current health cannot be reused
for the rotated credential. Multiple credential revisions at the same logical
endpoint remain distinct inventory records instead of collapsing. Later history may
group them by logical ID only for explicitly revision-aware analysis.

M1 uses identity version 1. Its logical ID is SHA-256 over a domain-separated,
length-delimited canonical encoding and is displayed as lower-case unpadded Base32
with an `ef1_` prefix. The private revision uses a separate domain and includes the
credential. Any semantic canonicalization change requires a new identity version;
old and new versions must not be joined implicitly.

The admitted M1 protocol slice is:

- VLESS with a UUID user ID, empty flow, direct TCP or WebSocket transport, and
  optional ordinary TLS;
- Trojan with a password, direct TCP or WebSocket transport, and required ordinary
  TLS.

The slice includes only WebSocket path, TLS server name, and certificate-verification
mode. Other protocol, transport, TLS, network, multiplexing, and dial fields are not
silently ignored; a future source adapter must reject them until the domain and both
renderers deliberately support them.

## Alternatives

Including raw credentials in an ID is an immediate disclosure. Publishing an
unkeyed credential hash still permits guessing attacks. A keyed fingerprint needs
a durable key lifecycle and would make identity depend on deployment key state.
A caller-assigned credential token would move equivalence and rotation correctness
into every adapter before there is an adapter contract. Network coordinates alone
would incorrectly reuse observations after credential changes.

## Consequences

The logical ID remains stable across display-name, source, metadata, and credential
changes, while TLS, transport, address, port, and protocol changes produce a new
logical ID. The confidential revision distinguishes credential changes without
becoming a public identifier. Storage introduced in M4 must define protected
encoding and migration for revisions before persisting them; M1 intentionally
exposes comparison, not revision bytes.

Deduplication operates on the full connection identity and verifies complete
configuration equality even after a key match. A mismatch is a deterministic
conflict, including the theoretical digest-collision case, and never selects an
input by arrival order. Detailed normalization and provenance rules remain in the
[endpoint design](../designs/endpoints-and-sources.md).
