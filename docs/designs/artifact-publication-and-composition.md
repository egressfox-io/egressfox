# Artifact publication and composition design

Status: accepted future architecture, refining the implemented M3/M6/M7 paths.
[ADR 0016](../decisions/0016-artifact-publication-and-availability.md) owns the
artifact lifecycle and availability decisions; [ADR 0017](../decisions/0017-leaf-pools-and-policy-composition.md)
owns leaf inventory and candidate composition. This document owns future
cross-target publication, no-op/repair and availability semantics. It does not
replace the detailed current renderer or Kubernetes contracts.

## Implemented today, accepted future, open

| Area | Implemented today | Accepted future direction | Still open |
| --- | --- | --- | --- |
| Artifact | Exact-profile render/native validation; protected content receipt; deterministic renderer subset | One validated artifact is an independent input to activation and zero-or-many publishers | Future shared artifact/publication interfaces and target correlation details |
| Output | Core file publisher; owner-checked BYO `outputSecretName` Secret | Shared Secret, Vault, S3-compatible, file and stdout publishers; future `EgressOutput` | API migration, auth, backend atomicity/retry/retention/status |
| Managed transport | M7 immutable exact-generation Secret mounted read-only; `Published`, `Activated`, `RuntimeReady` evidence | Internal staging stays separate from external output resources | Future status vocabulary for distinct external publication |
| Pool composition | One or more sources can contribute to a `ProxyPool`; current Gateway selects one pool | Leaf ProxyPools; candidate sets compose multiple Pool/Profile inputs | Candidate group schema, filter language and validation |
| Reconcile | Existing artifact/receipt checks suppress duplicate publication for current backends | Same desired artifact never creates a new generation/restart; missing/drifted target may be repaired at same generation | Cross-backend receipt and drift verification |
| Availability | M7 one replica, surge 1/unavailable 0, stable Service, previous-ready retention on failed replacement | Ready candidate before retiring LKG; readiness of usable local dataplane; best-effort drain | Exact readiness, signal, drain and standalone handoff mechanisms |

Do not read the future column as a claim that output adapters, EgressOutput,
multi-pool policy or new rollout behavior ship today.

## Artifact pipeline and identities

```text
Sources -> inventory -> evidence -> selection -> policy -> render
                                                   -> native validation
                                                   -> Validated Artifact
                                                        |             |
                                                managed activation  external publication
```

An artifact is publishable only after the exact engine/version-profile native
validator accepts its exact bytes. Probing is a separate operation: endpoint probes
measure reachability through a real engine/profile; native validation checks that
the engine accepts the generated config. Neither substitutes for the other.

The validated bytes and compatibility profile are the common payload. Activation
and publishers do not independently re-render. A valid artifact may be activated,
externally published, both, or neither while reconciliation is incomplete.

Internally, compare exact content/profile using a protected content identity or
fingerprint. Publicly correlate desired, materialized, published and active state
using a safe opaque generation/revision. Never publish a credential-derived config
digest by default in status, labels, annotations, metrics, logs or resource names.
The existing M7 random opaque generation names follow this boundary. A digest proves
equality only; it does not prove authenticity or protect guessable credentials.

Keep these revisions separate: source snapshot, endpoint connection credential,
observation/evidence, selection decision, validated artifact generation, external
publication state and active runtime generation. An observation or source refresh
does not automatically become an artifact generation.

## Publication destinations and ownership

The future universal publication family is:

- Kubernetes Secret;
- Vault;
- S3-compatible object storage;
- filesystem;
- stdout for explicit one-shot export.

These are shared EgressFox publisher capabilities, not Kubernetes outputs. Future
`egressfox generate`, `egressfox run` and `egressfox-operator` frontends may use the
same underlying semantics where their execution environment can reach a destination.
Operator stdout is excluded as a routine backend because controller output is not a
safe secret transport. Stdout export is sensitive and diagnostics remain on stderr.

Publishers consume the same validated engine payload. Backend envelopes can differ
(Secret data key, Vault document, S3 object, file); they do not re-render the engine
config. Correlate writes to one safe EgressFox artifact generation and retain
backend-native receipts separately. S3 may eventually support a current object and
optional immutable history; Vault and Kubernetes have different native version
semantics. Do not force a fake common version model.

`EgressOutput` is the preferred future Kubernetes resource for external publication.
Each output refers to one gateway artifact and owns one destination plus independent
reconciliation/status. Zero, one or many outputs may consume the same artifact. An
output add/remove/failure does not alter selection or require a new managed runtime
generation. Basic managed activation does not require EgressOutput: M7's internal
immutable exact-generation Secret remains its own transport.

Today, BYO `EgressGateway.spec.outputSecretName` is the existing user-facing output
contract; managed mode uses an internal generation Secret. Preserve those meanings.
Do not claim that either is already `EgressOutput`. A later API design must decide
alpha migration, stored objects, required/optional behavior and downgrade/rollback.
Specs may contain destination address, bucket, path/key/prefix, role and
non-confidential options. Tokens, passwords, keys and equivalent secret values are
external references, never inline CRD fields. There is no reusable `EgressSink`,
`BackendConnection` or `DestinationProvider` resource planned now.

Example external ownership boundary:

```text
EgressFox -> render + native-validate sing-box artifact -> Vault
                                                  |
                              External Secrets copies to Kubernetes Secret
                                                  |
                         consumer-owned GitOps/reloader/restart process
                                                  |
                         independently managed sing-box workload
```

EgressFox maintains the desired validated config in Vault but does not own or
activate that workload. The consuming organization owns its activation behavior.
File-to-systemd and S3-to-VM are similar supported-in-principle topologies.

## Change-driven publication and repair

Equivalent logical desired state must render deterministic canonical bytes. Exclude
reconcile timestamps, resourceVersions, process-local randomness and irrelevant
order; stable-sort only where order has no semantics. Ordered policy rules remain
ordered. The renderer compatibility profile is part of equality.

When current target ownership, bytes and profile match the desired validated
artifact:

- preserve the current safe generation;
- do not create a new artifact generation;
- do not reactivate or restart the managed data plane;
- do not mutate a destination that is already satisfied.

Reconcile frequency is not a deployment trigger. Source refresh, probe completion,
small latency changes, harmless input ordering, status writes, controller restart
or resourceVersion changes that leave the canonical artifact unchanged must not
roll out the Gateway. M8–M12 preserve this invariant.

No-op does not mean no repair. Reconciliation may restore the same generation when
an internal immutable generation Secret, external destination or protected receipt
is missing/mismatched. Adding a new EgressOutput may publish the current artifact to
that output. These repairs do not change selection, create a new dataplane
generation or restart a healthy runtime. Outputs are written only when the artifact
changed, an output is newly configured, its destination is missing/drifted, or its
receipt needs repair. A periodic reconcile alone is insufficient reason to write.

Kubernetes Secret updates, Vault versions, S3 PUTs and file modification times can
trigger downstream automation. Confirm satisfaction before mutation, and protect
receipt state sufficiently to recover no-op behavior across controller restart.
Backend ETag/resourceVersion/Vault version can aid diagnosis but does not replace
EgressFox's safe generation. Private content fingerprints may be used internally
where necessary and must not leak through public status.

## Output failure isolation

One output may fail while another succeeds. Report and retry failures within
backend-specific bounds. A Vault/S3/Secret publication outage does not remove or
stop an already healthy managed LKG. If a future policy makes an output mandatory
before accepting a new generation, its failure cannot destroy the previous healthy
generation while the candidate is unavailable.

Future status should distinguish artifact desired/validated, internal materialized
generation, active generation and each output's publication. The current M7
`Published` Condition means exact validated bytes and receipt exist in its owned
internal generation Secret. Keep that implemented meaning in current docs; any
future condition/resource naming is API design, not a docs-only rename.

## Availability-preserving activation

The current healthy LKG remains serving until a validated replacement is proven
runtime-ready. Lifecycle conceptually follows:

```text
active N -> derive and validate N+1 -> materialize N+1 -> start candidate
         -> prove usable local dataplane -> serve new connections on N+1
         -> stop assigning new connections to N -> drain N -> retire N
```

If candidate start/readiness fails, retain N; mark the candidate failed/degraded.
Publication success, Secret materialization or process existence alone is not
activation or readiness.

M7 already deploys one steady-state replica with `RollingUpdate`,
`maxUnavailable: 0`, `maxSurge: 1`, stable Service selectors and immutable
generation-bound configuration. Its fixed `egressfox-healthcheck` authenticates a
local SOCKS5 handshake. It preserves an old Ready replica while a new one is
unready and reports prior RuntimeReady separately from the not-yet-Activated
desired generation. This implementation must not be misdescribed as stop-then-start.

Future readiness should verify correct engine/profile, live process, expected bound
listener, the local authentication contract and acceptance of expected local client
traffic. It need not require external destinations to be reachable. Probe evidence,
not Pod readiness, owns endpoint/remote target health. The exact readiness test
remains open; do not claim M7 proves forwarded application traffic.

Keep the managed Service name/identity stable across config generations. Keep
client-facing SOCKS credentials stable across selection/config changes; credential
rotation is its own lifecycle. Minimize existing connection interruption where
possible: after the candidate is Ready, stop sending it new connections, allow a
bounded future drain period, request graceful engine shutdown, and force termination
only after a bounded timeout. Research actual Mihomo/sing-box signals, Service
endpoint behavior, readiness termination, preStop use and termination grace before
implementation. Do not invent timeout values or claim every arbitrary long-lived
session survives process replacement.

Standalone `egressfox run` follows the same healthy-LKG-until-ready rule. Its exact
listener/port handoff outside Kubernetes remains an open implementation problem.

## Pool and policy composition

The future candidate flow is:

```text
Sources -> leaf ProxyPools -> target-aware Profiles -> candidate composition
        -> EgressPolicy routing intent -> EgressGateway desired artifact
```

One ProxyPool may have multiple sources. A Profile evaluates the same leaf inventory
under a target/probe/selection context; different destinations do not require
duplicate inventories. Candidate groups may compose multiple `(Pool, Profile)`
inputs. Pools do not contain other pools. Composition belongs to selection/policy,
not source ownership. An EgressPolicy may refer to named groups for bounded routing;
no separate ProxyGroup CRD is justified at this stage. EgressGateway remains the
runtime/desired artifact association. Exact schema stays open under M9/M10 and Q16.

On composition, canonical full endpoint identity is deduplicated before eligibility
and scoring and provenance is unioned. Credential revisions remain distinct
connections, and observations cannot cross credential/profile/target/vantage
boundaries. Existing endpoint/source semantics remain in
[the source design](endpoints-and-sources.md) and
[selection design](observations-and-selection.md).

## Open API and implementation details

- `EgressOutput` fields/status, Gateway `outputSecretName` migration, required versus
  optional semantics, credentials/auth references and retry/ownership behavior.
- Vault KV generation and version support; S3 key layout, conditional write/ETag,
  current/history retention; file atomicity; output receipt persistence and drift
  checks.
- Whether one destination is required before accepting a new artifact; behavior
  across partial multi-output success.
- Exact candidate group/filter/profile reference schema and empty/direct/block
  semantics; these remain M10 design work.
- Readiness proof, active-vs-desired status fields, engine process signals, drain
  timeout and standalone listener handoff.
- Whether a reusable destination connection object becomes useful after real
  repeated use cases.

## Related authority

- [Runtime and standalone](runtime-and-standalone.md)
- [Policy, renderers and current publication](policy-rendering-publication.md)
- [Kubernetes API and current activation](kubernetes.md)
- [Endpoints and sources](endpoints-and-sources.md)
- [Observations and selection](observations-and-selection.md)
- [Threat model](../security/threat-model.md)
- [P1 and post-P1 roadmap](../roadmap/p1.md)
