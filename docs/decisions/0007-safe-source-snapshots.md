# ADR 0007: Bounded source snapshots with transactional replacement

Date: 2026-09-19. Status: accepted.

## Context

M2 introduces untrusted subscription bytes and remote acquisition before any
long-running reconciler or persistent source cache exists. The source boundary must
separate where bytes came from, how they are decoded, and how admitted records enter
the M1 inventory. A failed refresh must not look like an authoritative empty source,
and accepting some records from a damaged subscription by default can silently
remove working endpoints.

The first slice must exercise real acquisition and protocol semantics without
expanding the endpoint model or importing engine-native configuration schemas. The
VLESS sharing-link proposal and Trojan/Trojan-Go URL documentation were reviewed on
2026-09-19. Their broader option sets exceed M1: WebSocket Host headers, ALPN,
Reality/XTLS, fingerprints, flow, multiplexing, plugins, and alternate transports
cannot be discarded without changing connectivity.

## Decision

M2 implements inline bounded bytes and bounded HTTP/HTTPS acquisition. Acquisition
produces bytes plus advisory media metadata; it does not parse them. HTTP source
locations and headers are private configuration, while a caller-provided M1
`SourceID` is the only source identifier used in diagnostics and provenance. HTTPS
is the default scheme; plain HTTP and non-public destinations require separate,
explicit options. Destination policy is applied to every resolved dial address and
every redirect. Redirects are bounded and same-origin, and authorization headers
never cross an origin boundary.

The first formats are a line-oriented URI list and one non-recursive Base64 envelope
around that list. Callers may select either format explicitly or request conservative
auto-detection. Auto-detection recognizes direct supported/known proxy schemes first;
otherwise it accepts Base64 only when strict decoding yields a recognizable URI
list. Ambiguous or unrecognized bytes fail at source level. Base64 is an encoding,
not a separate nested subscription language.

The admitted records are the M1 VLESS and Trojan subset:

- VLESS UUID authentication, `encryption=none`, empty flow, TCP or WebSocket with
  explicit path, and `security=none|tls` with optional SNI and verification mode;
- Trojan password authentication, required TLS, TCP or WebSocket with explicit path,
  optional SNI, and verification mode.

Provider fragments become aliases only after M1 alias validation. Query keys that
affect connectivity are parsed explicitly; duplicates, unknown keys, engine-specific
options outside the slice, and unsupported values are rejected rather than ignored.
Known schemes or capabilities outside the slice are classified as unsupported;
broken syntax or field encoding is malformed; values rejected by M1 semantic
validation are invalid. Diagnostics contain only source ID, stable record position,
category and bounded reason code, never raw records or parsed secret values.

Parsing is record-oriented but snapshot admission is transactional by default. A
snapshot commits only when every non-comment record is accepted. Mixed accepted and
rejected input returns bounded diagnostics and leaves the previously committed
snapshot unchanged. Explicit opt-in partial admission may commit accepted records;
an all-rejected non-empty source is always a failed attempt. A syntactically valid
empty source is distinct from failure and commits only when the caller explicitly
allows empty replacement.

A committed `Snapshot` is the complete current contribution of one source. Replacing
it removes source-local relationships absent from the new snapshot; rebuilding the
inventory from all committed snapshots retains an endpoint while any other source
still contributes it. Removing a configured source is a separate explicit operation.
URI formats have no provider-stable record key, so M2 uses the safe logical endpoint
ID as the source-local record ID. The full connection identity still distinguishes
simultaneous credential revisions; credential rotation deliberately retains the
logical source-local key.

M2 uses fixed documented safety defaults: 4 MiB acquired/encoded bytes, 4 MiB
decoded bytes, 10,000 records, and 16 KiB per record. Configuration may lower these
limits or deliberately raise them within implementation ceilings. No decompression,
retry, conditional request, persistent cache, file watcher, Kubernetes Secret
reader, YAML, or JSON engine format is part of this decision.

## Alternatives

Mihomo YAML or sing-box JSON would add a dependency and a much wider semantic
surface before rendering capability is known. A URI-only slice maps directly to M1
and still proves the acquisition/parser/admission boundary. Always accepting partial
input makes temporary corruption authoritative; always rejecting partial input is
the safe default while opt-in supports providers with known heterogeneous records.
Using line numbers as record IDs would make provenance change under harmless
reordering. Treating empty bytes as failure would prevent deliberate source clearing;
committing every empty response would make truncation destructive.

## Consequences

Future file and Kubernetes Secret adapters can supply the same bounded byte payload
without changing parsing or endpoint semantics. A future reconciler can atomically
replace one source snapshot and preserve other provenance. Safe structured counts
are usable by a CLI, metrics adapter, or Kubernetes Conditions without exposing
subscription content.

HTTP destination controls reduce SSRF exposure but do not authorize endpoint or
probe destinations, and allowing private networks is explicit trusted deployment
intent. DNS policy is enforced on the addresses actually passed to the dialer;
deployment egress controls remain necessary defense in depth. P1 retains ETag,
Last-Modified, retries, content caching and richer source adapters. M3 revisits the
protocol slice against exact renderer versions without changing M2's rule that
unsupported connection semantics are rejected.
