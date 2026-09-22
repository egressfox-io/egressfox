# ADR 0016: Change-driven artifacts, independent publication, and safe activation

Date: 2026-09-23. Status: accepted future architecture refining existing boundaries.

## Context

ADR 0005 and ADR 0008 establish exact-byte validation, recoverable publication and
last-known-good output. ADR 0010 binds selection checkpoints to protected
publication receipts. ADR 0011 defines current BYO Secret output. ADR 0013 defines
the implemented M7 immutable generation Secret, stable Service, surge rollout and
exact-generation activation. Those records remain authoritative for their original
scope; the current `Published` Condition includes internal materialization used by
managed activation.

The product needs the same desired validated artifact to serve a managed runtime,
be published to external destinations, do both, or temporarily do neither. It also
needs stable no-op behavior and failure isolation across source/selection changes,
managed activation and independent outputs.

## Decision

### Artifact lifecycle and identity

A rendered configuration becomes a publishable **validated artifact** only after
the exact engine/profile validator accepts the exact bytes. The logical artifact is
separate from its deployment targets. It may be activated, externally published to
zero or more destinations, both, or neither while reconciliation is incomplete.
Publishers and activation consume the same validated payload; a destination does
not render its own engine configuration.

Keep two identities distinct:

- A private internal content fingerprint may compare exact secret-bearing bytes and
  compatibility profile for equality and protected receipts.
- A safe opaque artifact generation/revision correlates desired, published and
  active state without exposing content equality.

Do not expose content-derived fingerprints of credential-bearing configuration in
CRD status, labels, annotations, metrics, logs, resource names or other public
metadata by default. Public identity is not a digest of secret-bearing bytes.

Activation means a managed EgressFox runtime serves the exact generation. External
publication means the validated payload is available at a requested destination.
Neither implies the other. Existing M7 `Published` status terminology is preserved
for current behavior; future API design must distinguish internal staging for
activation from external `EgressOutput` publication without renaming current
Conditions in this documentation pass.

### Shared, deployment-independent publication

The future first-class publication family is Kubernetes Secret, Vault,
S3-compatible object storage, filesystem and stdout. These are EgressFox publisher
capabilities, not Kubernetes-owned concepts. Future one-shot/long-running CLI and
the operator can compose the same publisher semantics where the destination is
available. Operator stdout is not a normal output because controller logs/streams
are not a safe secret transport. Stdout is a sensitive one-shot export; diagnostics
go to stderr.

Render once, validate once and send the same engine payload to zero or more
publishers. Backend-specific envelopes and native versioning differ. A publisher
must avoid unnecessary destination changes: if the destination is confirmed to
contain the expected current artifact, reconciliation does not rewrite it merely
because it ran. Publication receipts retain enough protected state and backend
receipts to recover no-op behavior and diagnose success after restart. Backend
ETag/resourceVersion/Vault version does not replace EgressFox's safe generation.

Publication defaults are change-driven and repair-driven. Publish when the artifact
changes, a destination is newly configured, the destination is absent/drifted, or
its receipt requires repair. Reconcile frequency, source refresh, observation/status
activity and controller restart are not write triggers by themselves. Exact
artifact bytes must be deterministic for equivalent logical desired state; exclude
timestamps, resource versions and irrelevant ordering.

### Future Kubernetes output resource and current compatibility

The preferred future Kubernetes direction is zero, one or many `EgressOutput`
resources referencing one `EgressGateway` artifact, each with its own destination,
reconciliation and status. Adding/removing an output or an output failure does not
change selection or create a dataplane generation. One output may fail while another
succeeds. Basic managed Gateway activation never requires an `EgressOutput`: its
internal exact-generation Secret remains an activation transport.

Current API behavior stays explicit. BYO `EgressGateway.spec.outputSecretName`
publishes the existing owner-checked mutable Secret. Managed M7 uses an internal
immutable generation Secret. Neither behavior is retroactively described as
`EgressOutput`. A later alpha API design must deliberately decide field migration,
stored-object handling and downgrade/rollback behavior. Exact schema, status and
migration are open. Destination address/bucket/path/role and non-secret options may
be API configuration, but tokens, passwords and keys are references to external
credential material, never inline CRD secrets. No reusable `EgressSink`, backend
connection or destination-provider resource is currently planned.

An externally managed dataplane is a first-class use case: EgressFox may validate
sing-box configuration and publish it to Vault; External Secrets may copy it into a
Kubernetes Secret; consumer-owned GitOps/reload automation may update an independently
managed sing-box workload. In that topology EgressFox neither owns nor activates
the workload. Similar file-to-systemd and S3-to-VM flows are natural. External
publication credentials need not be given to managed Gateway Pods.

External output failure is isolated from the active dataplane. An output may be
degraded while the healthy Gateway keeps serving. Even if a future policy makes an
output mandatory before accepting a new generation, its failure does not destroy
the previous healthy LKG.

### Change minimization and same-generation repair

Equivalent deterministic desired validated artifacts produce the same private
content identity. If the current generation already matches, do not create a new
artifact generation, managed runtime activation or dataplane restart. Reconciliation
frequency is never a rollout trigger. Source snapshots, probe results, small latency
changes, status updates, Kubernetes resource versions, controller restarts, or
source/order permutations alone do not trigger a rollout when final validated
artifact bytes/profile are unchanged.

Artifact equality does not forbid all writes. Missing or drifted internal
materialization or external destinations may be repaired by republishing the same
exact artifact under the same generation. A newly added output receives the current
artifact without changing Gateway generation or restarting the runtime. Repairing
destination state is separate from changing dataplane state.

Keep source snapshot, endpoint connection, observation/evidence, selection decision,
validated artifact, external publication and active runtime identities separate.
No single global revision increments for every event. Private equality checks must
also confirm target ownership and compatibility profile before deciding no-op.

### Availability-preserving managed activation

The active healthy last-known-good runtime remains available until a replacement has
been validated and proven runtime-ready. A new artifact being rendered, validated,
published or materialized does not mean it is active. If candidate activation
fails, the old healthy generation remains active and the new candidate is degraded.

Kubernetes implementation must preserve surge-first semantics. M7 already configures
one steady-state replica, `maxUnavailable: 0`, `maxSurge: 1`, a stable Service and
retains the old ready process while a replacement is unavailable. Future
implementation must keep that direction and prove readiness before retiring the
old generation; steady-state replica count is not a reason to use `Recreate` or
reduce Ready capacity to zero.

Future readiness must show that the expected engine/profile and runtime are healthy,
the listener is bound, the local client-auth contract works, and the candidate can
accept expected local traffic. It need not depend on arbitrary public Internet
reachability; remote path evidence belongs to endpoint probes. M7 currently runs a
fixed helper that reads mounted credentials and negotiates authenticated SOCKS5 on
loopback. That is useful local evidence, not a claim of arbitrary endpoint or
application health.

Keep Gateway Service identity stable across artifact generations. Client-facing
credentials have a lifecycle separate from selected endpoint/config generations
and must not rotate merely because the artifact changes. Where engine/platform
behavior permits, stop assigning new connections to the old generation, allow
existing sessions to drain for a bounded future grace period, then terminate
gracefully and force termination only after a future bounded timeout. Do not claim
that every arbitrarily long-lived TCP/UDP/application session survives process
replacement. Service-level availability during a controlled rollout is distinct
from universal preservation of every connection.

The same active-LKG-before-replacement-ready principle applies to future standalone
`egressfox run`. Kubernetes Service behavior does not decide bare-host listener
handoff. The exact standalone mechanism remains open.

## Alternatives rejected

- Treating publication as activation conflates an output write with a process
  loading and serving exact bytes, and excludes externally managed runtimes.
- Re-rendering per destination can create different engine configs from one
  decision and loses exact-artifact correlation.
- Writing every reconcile creates downstream restarts even when the artifact is
  identical.
- Making same-content state completely immutable prevents repair of deleted or
  drifted destinations without requiring a new dataplane generation.
- One generic reusable `EgressSink` is premature without demonstrated connection
  reuse needs.
- Stopping the old managed generation before starting a replacement discards the
  current LKG on failure and creates avoidable service interruption.

## Consequences and unresolved details

Current M3 file publication, M6 BYO Secret output and M7 managed activation remain
implemented as recorded in their existing ADRs. New Vault/S3/Secret/file/stdout
shared publisher APIs and `EgressOutput` are future. Exact output schema and
migration, required/optional behavior, backend authentication/atomicity/retries,
receipt/status/retention, destination drift checks, readiness fields, engine signal
behavior, drain duration, standalone handoff and non-drainable-session behavior
remain open design/implementation questions. Reusable output connection resources
are not planned.
