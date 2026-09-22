# Policy, renderers, and safe publication

Status: accepted boundaries with the M3 renderer, native-validation and file
publication contract fixed by ADR 0008. The
[artifact publication and composition design](artifact-publication-and-composition.md)
owns accepted future cross-backend publisher, no-op/repair and availability
semantics; this document remains authority for the current renderer and publication
contracts.

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

Research dates: 2026-09-18 and 2026-09-19. Releases inspected and tested: Mihomo
[v1.19.31](https://github.com/MetaCubeX/mihomo/releases/tag/v1.19.31) and sing-box
[v1.14.1](https://github.com/SagerNet/sing-box/releases/tag/v1.14.1). Documentation
may describe development features newer than these releases. These two exact
versions form the implemented M3 compatibility matrix; other versions are rejected
until deliberately qualified.

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

Artifact metadata includes the engine/version profile, renderer schema, media type,
exact-byte generation and bounded validator identity. Input/desired revisions and
intended targets remain future reconciliation state rather than artifact content.
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
`sing-box check`. The M3 native checker runs these exact commands after verifying
the executable's version. Official release assets are opt-in test prerequisites,
never downloaded by ordinary unit tests or retained in the repository.

The sing-box v1.14.1 renderer declares a `local` DNS server and sets it as
`route.default_domain_resolver`. This preserves DNS-host endpoint support now that
sing-box v1.14 requires an explicit resolver for outbound server domain names.

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

Future work may allow explicitly engine-scoped native additions and user base configurations.
They need a documented merge/ownership policy: reject reserved-field collisions,
duplicate tags, unsafe listeners/hooks, and attempts to override validated routing
or credentials without explicit permitted semantics. No opaque arbitrary deep merge.
Validate the final composition, not just EgressFox's generated portion. This path
does not grant subscription content authority over output configuration. Native
composition is deferred beyond the committed P1 path.

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

M5 exposes that protected current receipt to `internal/reconcile` without exposing
artifact bytes or a public digest. SQLite stores a pending decision/receipt before
file publication and promotes it only after publisher readback matches. Restart
promotes a matching pending checkpoint or discards a non-matching one; committed
selection state is reused only while its receipt still matches the current LKG.
This explicitly bridges the database/filesystem crash window without claiming an
atomic transaction. An evaluation older than committed state is rejected.

Use distinct concepts: candidate, validated, published, and activated. P0 LKG means
the last successfully published, validated artifact plus its provenance. In BYO
mode activation remains unknown; a publication Condition must not claim traffic
readiness. Future reload/activation adapters can report a separate acknowledgment.

P1 M7 makes that acknowledgment concrete only for an operator-managed runtime under
[ADR 0013](../decisions/0013-managed-gateway-activation.md). The common policy
listener now distinguishes loopback/unauthenticated BYO intent from Pod-network,
username/password-authenticated managed intent without Kubernetes types. Mihomo
renders `socks-port`, `allow-lan`, `bind-address` and `authentication`; sing-box
renders one SOCKS inbound with `users`. Both exact-profile native validators must
accept the result; neither renderer may omit authentication or add another listener.

The first activation path binds a fresh process Pod to an immutable generation
Secret and uses an authenticated loopback SOCKS handshake plus completed Deployment
rollout as activation evidence. M7 starts the engine executable directly; it does
not yet use the future `egressfox-runtime` wrapper. It does not enable Mihomo's
control API or assume sing-box `SIGHUP` has the same semantics. The previous ready
generation and its Secret remain until the new generation activates. BYO mode
remains publication-only.

P1 M10 expands the common model only after Q16 proves a bounded ordered rule/action
intersection for both exact engine versions. One Gateway references at most one
EgressPolicy, so policy precedence is explicit rather than a merge. The initial
target is named profile groups, ordered domain/CIDR/TCP-port matching and explicit
profile/direct/block final behavior; exact version tests may narrow, but never
silently approximate, that set.

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

There is no atomic transaction across target and state store. M3 tests first
publication, replacement/no-op, permissions and symlinks, an interrupted target
rename, target/receipt rollback after a sync failure, journal recovery, temporary
cleanup, cancellation and in-process concurrency. Filesystem exhaustion and a
cross-process lock are deployment/future composition concerns; one process owns a
target in M3.

### Target-specific constraints

**File (P0):** one writer per target; stage in the same filesystem, use restrictive
permissions (normally file `0600`, private directory `0700`), sync content and
directory according to the promised crash-durability contract, and atomically
replace. Reject unsafe symlink/path ownership. Retain the previous artifact
securely before replacement, with a bounded retention/cleanup policy. Do not
assume rename alone proves power-loss durability or cross-filesystem atomicity.

**Kubernetes BYO Secret (implemented M6):** the mutable target is type
`egressfox.io/engine-config`, has the exact EgressGateway controller owner, and
contains one engine config plus `.egressfox-receipt` in `data`. The receipt and
configuration replace atomically in one resourceVersion-guarded API update. An
unowned or differently typed object is rejected, identical bytes are a no-op, and
owner-reference garbage collection implements Gateway deletion. There is no second
backup Secret: the current owned object is the recoverable LKG. API-server durability
and mounted-file propagation remain Kubernetes responsibilities.
Mounted-file propagation and application reload are separate from API publication.

**Kubernetes managed generation Secret (M7):** each distinct validated artifact,
including the generated listener credentials, is one immutable, exactly owned
Secret with config plus protected receipt. A random API-server suffix supplies the
opaque generation name; no content digest crosses into metadata or status. Equality
is established by protected receipt readback, not a public annotation. The
Deployment template names that Secret, so a template revision and ready Pod bind
activation to exact bytes. Cleanup retains active/previous/Pod-referenced generations
and deletes older exact-owned objects. An ownership collision fails closed.

Kubernetes documents [Secret handling and size limits](https://kubernetes.io/docs/concepts/configuration/secret/).
Base64 is not encryption. Secret authorization and cluster encryption-at-rest
remain deployment responsibilities, with least-privilege operator access.

**Future publisher family:** Kubernetes Secret, Vault, S3-compatible object
storage, filesystem and stdout are accepted deployment-independent directions under
the [artifact publication design](artifact-publication-and-composition.md), but the
new adapters are not implemented. Stdout is a sensitive one-shot export, not a
normal operator sink; diagnostics belong on stderr. ConfigMap remains suitable only
for artifacts proven credential-free end to end. Each backend still needs its own
idempotency, authentication, acknowledgment, retry and recovery contract.

## Non-goals and open questions

No EgressFox runtime routing implementation, universal policy language, live engine
reload or native merge engine exists here. `EgressOutput` and Vault/S3/shared output
adapters remain future architecture. M7 compatibility is limited to the exact
profiles and the authenticated managed listener described above. Activation and
managed ownership are resolved by ADR 0013; the
[decision queue](../decisions/open-questions.md) retains native composition under
Q10. BYO Secret publication remains resolved by ADR 0011.
