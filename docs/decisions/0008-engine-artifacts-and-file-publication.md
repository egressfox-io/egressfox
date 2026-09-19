# ADR 0008: Pinned engine artifacts and journaled file publication

Date: 2026-09-19. Status: accepted.

## Context

M3 must turn the M1/M2 inventory into usable Mihomo and sing-box configurations
without making either engine's schema the EgressFox policy model. Rendered bytes
contain credentials. Syntax alone does not establish compatibility, and replacing a
working file before exact-byte validation can interrupt egress. A process can also
stop between replacing a target and recording that replacement.

The current stable upstream baselines reviewed on 2026-09-19 are Mihomo v1.19.31
and sing-box v1.14.1. Mihomo supports `-t -f <config>` test mode; sing-box supports
`check -c <config>`. Both represent the admitted VLESS/Trojan, TCP/WebSocket and
ordinary TLS fields, but use different native schemas.

## Decision

M3 introduces a small engine-independent desired gateway: a local SOCKS listener,
the complete caller-supplied admitted inventory, one deterministic selector over
that inventory, and an explicit final route to that selector. Empty inventories,
non-loopback listeners and unsupported policy or endpoint semantics fail before
serialization. Endpoint selection, routing rules, DNS policy, UDP, engine-native
health tests, balancing and native escape hatches remain outside this model.

Implement complete-configuration renderers for Mihomo v1.19.31 and sing-box v1.14.1.
They generate collision-safe endpoint names from the safe logical endpoint ID plus a
deterministic ordinal for simultaneous credential revisions. Names and output order
never use aliases or confidential revision bytes. Typed native structs are serialized
with deterministic field and inventory ordering. Mihomo YAML uses the maintained
`go.yaml.in/yaml/v3` library; sing-box uses the Go JSON encoder.

Rendering produces a secret-bearing candidate artifact. Validation is a separate
boundary and produces a distinct validated-artifact type bound to exact bytes,
engine/profile and validator evidence. Native validation is mandatory for supported
publication profiles. It runs the exact configured binary without a shell, verifies
the pinned version, uses a private temporary directory and candidate file, applies a
deadline and controlled environment, and replaces all child output with bounded safe
reason codes. Unit tests use deterministic checker doubles; release/compatibility
evidence uses checksum-verified pinned binaries.

The exact artifact bytes plus engine and compatibility profile form a SHA-256
generation. Because configuration can contain low-entropy credentials, this digest
is confidential: normal formatting, errors, metrics and public status show only a
redacted generation. Protected file receipts may store it because they share the
artifact's confidentiality boundary.

The local file publisher accepts only validated artifacts. It requires an existing,
private, non-symlink target directory and one writer per target. It creates `0600`
temporary files in that directory, syncs content, retains at most one protected
previous artifact, writes and syncs a journal, atomically renames the candidate over
the target, syncs the directory, reads the target back, atomically writes a protected
receipt, then removes the journal and syncs the directory again. The current target
plus matching receipt is the restart-recoverable LKG; the previous file is a bounded
recovery aid, not an automatically activated rollback.

On startup/publication, a journal is recovered before new work. If the target matches
the journaled validated generation/profile, the receipt is completed; otherwise the
old matching target/receipt remains authoritative and the incomplete attempt is
discarded. An existing unmanaged target is never adopted or overwritten. A matching
target, receipt and profile is a no-op. Cancellation is honored before commit; once
rename begins, receipt completion proceeds to avoid an ambiguous committed state.
Publication proves durable file replacement under the documented sync assumptions,
not engine reload or traffic activation.

The compatibility evidence used the official Darwin arm64 release assets. GitHub
release metadata reported SHA-256
`d131f44b3deb2a8356f7ac75048ad67a10d53243323951c4f3cda7b672922963`
for `mihomo-darwin-arm64-v1.19.31.gz` and
`b9024642ef7b4848252df5469b7f60ef3c18bb5e217a16a0934f0174f8ad11b4`
for `sing-box-1.14.1-darwin-arm64.tar.gz`; downloaded bytes matched. Both native
checkers accepted the reviewed goldens. A controlled test rendered, validated and
published the sing-box configuration, then carried an HTTP request over its SOCKS
listener through a local TLS Trojan server. This proves the M3 path for that fixture,
not general endpoint availability or production activation.

## Alternatives

Using native YAML/JSON as common policy couples every consumer to one engine.
Rendering only one engine contradicts the M3 roadmap and would leave the common
boundary unproved. Exposing the content digest enables offline guesses of weak
credentials. Rename without a receipt leaves restart ambiguity; receipt without a
journal cannot distinguish an interrupted commit. Automatically adopting an existing
file risks overwriting user-owned configuration.

## Consequences

Future selection can pass a chosen inventory into the same desired model. A future
Secret publisher can consume the validated artifact without knowing renderer details.
Additional engines implement the renderer/profile boundary, while publication remains
engine-independent.

Target directories and receipts are part of the secret boundary and require backup
protection. Cross-process locking is not provided; composition must enforce one writer
per target. Directory `fsync` and atomic rename guarantees depend on the supported
local filesystem and operating system. External validation does not prove endpoint
reachability or activation. Q6 and the file portion of Q7 are resolved here; Secret
publication and reload/rollback acknowledgment remain gated at their later milestones.
