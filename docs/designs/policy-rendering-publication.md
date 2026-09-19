# Policy, renderers, and safe publication

Status: accepted boundaries with the M3 renderer, native-validation and file
publication contract fixed by ADR 0008.

## Decisions

Common intent is engine-independent, but support is engine/version-specific.
Unsupported behavior fails explicitly; renderers must not approximate it silently.
Render deterministically, validate before publishing, and retain last-known-good
output. Credential-bearing output is sensitive even when encoded or checksummed.
See [ADR 0005](../decisions/0005-validated-publication.md).

## Policy direction

### M3 common slice

[ADR 0008](../decisions/0008-engine-artifacts-and-file-publication.md) defines the
implemented M3 slice: one loopback SOCKS listener, the caller-supplied admitted
inventory, a deterministic selector containing every record and an explicit final
route to it. M3 is TCP-only. Empty inventories and non-loopback listeners fail.
DNS policy, ordered destination rules, direct/block actions, multiple groups,
balancing, URL tests and endpoint selection are deferred rather than assigned engine
defaults.

The compatibility profiles are Mihomo v1.19.31 and sing-box v1.14.1. Both render
complete configurations for the M1 VLESS/Trojan, TCP/WebSocket and ordinary TLS
slice. Generated names use safe logical IDs and deterministic revision ordinals;
aliases and confidential revisions never become native names.

A desired gateway model combines the selected endpoint set, common routing intent,
engine compatibility profile, and target/runtime settings. Separate pool derivation
from policy composition so one engine's group vocabulary does not become the core
domain vocabulary. Validate references, cycles, defaults, and feasibility before
rendering. Stable generated tags must be collision-safe and independent of display
names; errors identify a safe model location.

Candidate concepts are named pools/groups, destinations, domain and CIDR matching,
transport protocols and ports, rule sets, direct routing, fallback, URL-test-like
selection, load balancing, and an explicit final/default route. P0 should select a
small useful subset and specify ordering and match semantics precisely. Do not
promise every candidate concept across both engines.

Rule ordering is semantic. Canonical output may sort unordered endpoint records
or map keys but must not alphabetize ordered routing rules or fallback lists.
Do not rely on an engine's implicit first outbound/default fallback when policy
requires explicit routing. DNS resolution location, DNS traffic routing, and
TCP/UDP support must be reviewed with each admitted rule/protocol feature.

## Upstream findings

Research date: 2026-09-18. Releases inspected: Mihomo
[v1.19.31](https://github.com/MetaCubeX/mihomo/releases/tag/v1.19.31) and sing-box
[v1.14.1](https://github.com/SagerNet/sing-box/releases/tag/v1.14.1). Documentation
may describe development features newer than these releases. These are research
baselines, **not a tested EgressFox support matrix**; pin exact binaries and fixture
versions when implementing M3.

| Concern | Mihomo model | sing-box model | Design consequence |
| --- | --- | --- | --- |
| Endpoint declarations | YAML `proxies`, optionally `proxy-providers` | JSON `outbounds` with type/tag; some protocols use `endpoints` | Render typed engine output; do not rename a common map's keys |
| Group behavior | `select`, `url-test`, `fallback`, `load-balance` groups | Documented `selector` and `urltest` outbounds | Similar names are not proof of identical behavior |
| Balancing | Explicit load-balance strategies | No equivalent general group established by this research | Reject balancing for sing-box unless the pinned version's equivalent is proven |
| Routing | Ordered rules targeting proxies/groups | `route.rules`, actions, `rule_set`, and `route.final` | Preserve precedence and final route explicitly |
| External endpoint sets | Native provider objects | Do not assume a Clash provider schema exists | Fragment output requires an explicit composition contract |
| WireGuard | Engine proxy configuration | Migration guidance moves legacy outbound to endpoint | Protocol modeling cannot assume every endpoint is an outbound object |

Mihomo documents [group references and health-check fields](https://wiki.metacubex.one/en/config/proxy-groups/),
[provider configuration](https://wiki.metacubex.one/en/config/proxy-providers/), and
[balancing strategies](https://wiki.metacubex.one/en/config/proxy-groups/load-balance/).
Provider health checks are not automatically equivalent to group health checks.
Do not implicitly enable extra engine probes on top of EgressFox's probe budget.

sing-box documents its [configuration structure](https://sing-box.sagernet.org/configuration/),
[selector](https://sing-box.sagernet.org/configuration/outbound/selector/),
[URLTest](https://sing-box.sagernet.org/configuration/outbound/urltest/), and
[route final behavior](https://sing-box.sagernet.org/configuration/route/).
Selector default and runtime selection are distinct; the docs identify the Clash
API as its control interface. A selector is not a load balancer. URLTest uses a
target and timing/tolerance settings, not EgressFox's historical adaptive scoring.
The [migration guide](https://sing-box.sagernet.org/migration/) and
[WireGuard endpoint model](https://sing-box.sagernet.org/configuration/endpoint/wireguard/)
show why engine-version compatibility is part of the contract.

### Capability contract

Every renderer must check protocol/transport/TLS variants, rule kinds, group
semantics, output mode, and target version before claiming support. P0 needs
internal capability checks and descriptive errors. A user-facing capability
matrix/discovery CLI is P1. A capability name alone is insufficient if semantics
change by version or build flags.

An unsupported-feature error should name the engine/version, model field and
requested behavior, with a safe reason. It must not emit endpoint URIs or native
config. Do not substitute selector for balancing, drop unsupported rules, downgrade
TLS, or route directly when a pool is empty. Partial render success cannot publish
a silently reduced policy.

## Rendering and validation

M3 represents candidate and validated artifacts as distinct secret-bearing types.
Only native validation bound to the exact bytes/profile produces a publishable
artifact. The generation is SHA-256 over engine, profile and exact bytes, but remains
confidential in memory and protected receipts because low-entropy credentials can be
guessed from a public digest. Validator subprocess output is never returned verbatim.

Proposed artifact metadata includes engine/version profile, renderer/model schema
version, input revision, content digest, validation result, and intended target.
Keep volatile timestamps out of payload bytes unless the format explicitly requires
them. A content digest supports equality, not authenticity or secrecy; do not expose
credential-derived digests publicly without a separate security decision.

Validation has layers:

1. Validate common intent and references; reject unsupported semantics.
2. Render via typed serializers, not string interpolation of untrusted records.
3. Parse/schema-check the resulting native artifact and enforce EgressFox safety constraints.
4. Validate with the exact supported engine binary/build and complete dependencies.
5. Bind the validation evidence to the exact bytes and target compatibility profile.

Mihomo's [CLI source](https://github.com/MetaCubeX/mihomo/blob/v1.19.31/main.go)
defines configuration test mode (`-t`, with configuration path `-f`); sing-box's
[configuration documentation](https://sing-box.sagernet.org/configuration/) documents
`sing-box check`. These are future integration-test tools, not installed prerequisites
or commands executed by this bootstrap. M3 must verify invocation, exit behavior,
build flags, auxiliary files, and time/resource limits with pinned binaries.

Engine validation may load external files, resolve names, or initialize resources.
Treat the validator as a restricted child process: no shell, deadline, bounded
sanitized stderr/stdout, private temporary files, controlled environment/filesystem,
and constrained network access. Do not log validator output verbatim. A successful
exit establishes compatibility/syntax within that environment, not destination
reachability or production runtime activation.

Complete configurations are the first proposed output mode. Future provider/proxy
or outbound fragments must be validated in their intended base composition;
standalone fragment syntax alone is insufficient. Record the base revision.

### Native escape hatches and composition

P1 may allow explicitly engine-scoped native additions and user base configurations.
They need a documented merge/ownership policy: reject reserved-field collisions,
duplicate tags, unsafe listeners/hooks, and attempts to override validated routing
or credentials without explicit permitted semantics. No opaque arbitrary deep merge.
Validate the final composition, not just EgressFox's generated portion. This path
does not grant subscription content authority over output configuration.

## Publication state and LKG

### M3 file protocol

M3 requires an existing private directory and rejects symlink targets and unmanaged
existing files. It stages `0600` files on the target filesystem, retains one previous
artifact, syncs content, writes a protected journal, atomically renames, syncs the
directory, reads back, writes a protected receipt and removes the journal. A matching
target and receipt is the restart-recoverable LKG. A journal lets restart complete a
committed receipt or discard a pre-commit attempt. One publisher serializes local
calls; composition guarantees one process/writer per target. No-op means exact bytes
and compatibility profile still match the owned target.

Use distinct concepts: candidate, validated, published, and activated. P0 LKG means
the last successfully published, validated artifact plus its provenance. In BYO
mode activation remains unknown; a publication Condition must not claim traffic
readiness. Future reload/activation adapters can report a separate acknowledgment.

Conceptual transition:

```text
coherent input snapshot -> render -> validate exact bytes
                                      |
                     recheck desired revision and target ownership
                                      |
               compare content/profile with current publication
                                      |
                stage securely -> commit target -> read back
                                      |
                record receipt and retain previous known-good
                                      |
                     activation acknowledgment if available
```

Validation failure never enters publication. Equality can skip the write only if
target content and compatibility profile still match and ownership is intact.
Keep the last successful artifact/receipt separate from a failed attempt's metadata.
A first-run validation failure publishes nothing and reports unready; it has no LKG
to fall back to. Empty/no-feasible selections require an explicit policy outcome,
not a syntactically valid accidental direct route.

Retaining LKG preserves a previous validated artifact; it does not establish
compliance with a newly changed policy or the continued validity of its credentials.
Report the desired/published revision mismatch and degraded condition. Explicit
credential revocation, expired-output limits, and emergency disable behavior must
be decided at Q7 so retention cannot silently override a security requirement.
Never invent an unvalidated replacement as a response to that conflict.

| Boundary failure | Required behavior |
| --- | --- |
| Render/validate fails | Keep current output and LKG untouched |
| Desired policy changes during work | Discard obsolete candidate; reconcile latest revision |
| Write fails before commit | Keep current target and old receipt |
| Commit succeeds but acknowledgment is lost | Read target back; compare bytes/profile/revision before retry or promotion |
| Process dies after target commit, before receipt | Recover from durable staged metadata/target identity; do not assume failure or overwrite newer output |
| Engine rejects/reload fails after publication | Preserve previous known-good bytes; report activation failure separately; rollback rules need their own design |

There is no atomic transaction across target and state store. M3 must design and
test the file journal/receipt recovery, including first publication, retries, disk
full, permissions, interrupted rename, and rollback retention. Selecting a target
adapter does not waive these requirements.

### Target-specific constraints

**File (P0):** one writer per target; stage in the same filesystem, use restrictive
permissions (normally file `0600`, private directory `0700`), sync content and
directory according to the promised crash-durability contract, and atomically
replace. Reject unsafe symlink/path ownership. Retain the previous artifact
securely before replacement, with a bounded retention/cleanup policy. Do not
assume rename alone proves power-loss durability or cross-filesystem atomicity.

**Kubernetes Secret (P0):** use a namespaced target with clear owner, explicit
data keys, and optimistic concurrency. Never adopt/overwrite an unrelated Secret.
A single-object update can replace a payload atomically at the API level; backup
and receipt updates across objects are not transactional. Review mutable target
plus retained previous Secret versus versioned immutable Secrets before M6. Check
payload size below the API limit and fail before mutation; do not auto-shard output.
Mounted-file propagation and application reload are separate from API publication.

Kubernetes documents [Secret handling and size limits](https://kubernetes.io/docs/concepts/configuration/secret/).
Base64 is not encryption. Secret authorization and cluster encryption-at-rest
remain deployment responsibilities, with least-privilege operator access.

**Future targets:** stdout is a sensitive export stream, not durable atomic
publication; diagnostics belong on stderr. ConfigMap is only for proven
credential-free artifacts. Vault and HTTP/webhook targets require target-specific
idempotency, authentication, acknowledgment, retry, and rollback contracts.

## Non-goals and open questions

No runtime routing implementation, universal policy language, live engine reload,
native merge engine, or claim of compatibility exists here. Q6–Q7 in the
[decision queue](../decisions/open-questions.md) cover the common subset, compatibility
matrix, LKG recovery, empty-pool behavior, and activation/rollback contracts.
