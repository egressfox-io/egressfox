# Threat model

Status: design requirements with M1 domain redaction, M2 bounded source controls,
M3 secret-bearing artifact/native-validator/file-publication controls, M4 probe
authorization/budget/history controls, and M5 revision/context-bound selection plus
receipt-bound decision checkpoints implemented. M6 adds same-namespace Secret
references, owner-checked Secret publication, scoped RBAC, non-root containers and
RWO-PVC single-active operation.
Release tooling adds a centralized checksum manifest, exact engine/source/license
artifacts, SPDX SBOMs, image/package scanning, keyless signing and provenance.
Ordinary CI is read-only; publication is tag-bound and protected-environment gated.
There is no managed data-plane runtime or production stability claim. Reporting
guidance is in [SECURITY.md](../../SECURITY.md).

The [P1 roadmap](../roadmap/p1.md) is design, not an implemented control. Its first
milestone introduces a new authenticated proxy-listener and managed-workload trust
boundary only after Q7/Q13 resolve exact-generation activation and ownership.

M2 HTTP acquisition permits HTTPS by default, requires explicit intent for HTTP and
non-public destinations, checks resolved addresses on the actual dial path, disables
environment proxy use, limits same-origin redirects, time and bytes, and exposes only
safe source IDs/reason codes. M4 independently resolves and authorizes every endpoint
and probe-target answer, pins engine/HTTP dials to deterministic allowed literals,
and requires separate trusted intent for private endpoint and target ranges. Deployment
egress policy remains required defense in depth. M3 native validation pins the exact engine
profile, runs without a shell in a private temporary directory with a deadline and
controlled environment, and reduces child output to safe reason codes. The M3 file
publisher requires a trusted private directory, rejects symlinks and unmanaged
targets, uses `0600` files, and keeps protected receipt/journal/previous state in
that same secret boundary. M4 bounds jobs, queues, concurrency, process readiness,
requests, response bytes and SQLite retention; its diagnostics omit engine output,
raw targets, credentials and confidential revisions. M5 explanations expose safe
logical endpoint IDs and bounded integer evidence components only; selection state,
private revisions and artifact receipts remain in the protected SQLite/file boundary.

## Assets, actors, and trust boundaries

Assets are source credentials, endpoint credentials/private keys, generated engine
configuration, Kubernetes access, state integrity, publication ownership, network
access from probes, and the availability/integrity of production egress.

Assume subscriptions and endpoints can be malicious or compromised. Probe targets
can fail or lie. An authorized configuration author can make mistakes; possession
of a subscription URL does not authorize all network destinations it contains.
CI contributions are untrusted until reviewed. P0 is not a multi-tenant isolation
product and cannot defend against an administrator who controls the host/cluster.

Trust boundaries:

1. Operator/user policy and credential references enter configuration adapters.
2. Remote/file content enters bounded fetchers and parsers.
3. Normalized endpoints enter network-capable probe engine workers.
4. Observations enter history and the decision function.
5. Desired models enter serializers and external native validators.
6. Validated artifacts cross filesystem/API publication boundaries.
7. The data-plane process consumes sensitive artifacts and handles application traffic.

## Threats and required controls

| Threat | Required design and tests before feature ships |
| --- | --- |
| URLs/URIs or generated configs leak through logs, errors, Events, metrics, CLI diff | Safe identifiers, redaction at construction, bounded reason codes, no raw serializer/validator errors; adversarial credential canaries through all diagnostics |
| Source URL SSRF and redirect credential leakage | Explicit allowed schemes/destination policy, resolution-time and dial-time checks, bounded redirects, origin-scoped auth headers, timeout/byte limits |
| Endpoint/probe SSRF, including private networks and cloud metadata | Separate authorization for fetched endpoint addresses and probe targets; block loopback/link-local/private/control networks by default for untrusted input, deliberate allow rules for authorized internal use |
| DNS rebinding or proxy-resolved destinations bypass host checks | Enforce policy on actual dial paths including IPv6/mapped addresses; constrain engine worker egress, understand remote resolution; do not claim protection from URL string checks alone |
| Oversized/malicious subscription or parser exhaustion | Stream limits before allocation, encoded/decompressed/decoded limits, record/depth limits, time bounds, bounded YAML aliases, reject duplicate keys/ambiguous formats; fuzz under limits |
| Excessive network load or resource exhaustion | Bounded queue, global/per-target concurrency and rate limits, explicit retry budgets, cancellation, worker lifecycle cleanup, body limits and scheduling-load tests |
| Compromised endpoint observes probe/application traffic | Secure TLS verification, no real application credentials in test requests, minimal synthetic probe payload; a successful probe does not establish endpoint trust |
| Configuration injection or native escape-hatch abuse | Typed serializers, trusted-only native settings, reject reserved collisions and unsafe hooks/listeners; validate final composed bytes with restricted native processes |
| Unauthorized/unintended output replacement | Target ownership, exact validation evidence, generation checks, atomic/recoverable publication, conflict handling, LKG retention and interruption tests |
| Files, DB backups, temporary output, or crash dumps expose secrets | Private directories, restrictive modes, symlink checks, bounded retention, minimal credential persistence, no sensitive CI artifacts; document host/backup protection |
| Kubernetes Secret exposure/excessive RBAC | Same-namespace explicit references, scoped reads/watches and writes, no raw values in status; no broad list/watch if not required; test negative authorization paths |
| Engine control API or proxy listener exposed | Bind probe control interfaces privately, authenticate where applicable, do not create an open proxy; keep control and application listeners distinct |
| Dependency/build compromise | Review new dependencies, pin Actions and tool versions, checksum downloaded executables, `govulncheck`, scoped CI permissions; image scanning/signing/SBOM before release pipeline |
| Compromised upstream engine release or checksum drift | Review exact tag/license/commit, pin source and reference-asset SHA-256 in one manifest, fail closed before extraction, build from source, retain release SBOM/source evidence; residual risk remains if upstream source and release account were compromised together before review |
| Malicious Go dependency or dependency confusion | `go.mod`/`go.sum`, controlled module proxy/sum database behavior, code review, `govulncheck`, binary SBOM and no dynamic plugin loading; checksums establish identity rather than trustworthiness |
| Base-image/package mutation | Pin OCI base digest and CA package version, copy only the needed CA bundle into the final layer, generate/scan final-image SBOM and review digest changes |
| Compromised GitHub Action or untrusted pull request seeking credentials | Full Action commit SHAs, `persist-credentials: false`, read-only PR/validation permissions, no PR release job, exact-tag check, protected `release` environment and job-local write/OIDC permissions |
| Registry, signing identity or release-workflow compromise | Verify immutable digest plus expected repository/workflow/tag certificate identity and provenance; protected reviewers and transparency records limit but do not eliminate maintainer/GitHub compromise |

## P1 design gates

The following controls are required by the P1 milestone that introduces each new
boundary; they must not be described as present before that implementation ships.

| Planned boundary | Required control before shipment |
| --- | --- |
| Managed proxy Service | Secret-backed client authentication, ClusterIP only, CNI-dependent NetworkPolicy as defense in depth, no engine control API exposure, non-root/read-only/drop-all container and exact owner collision checks |
| Generation activation | Immutable revision-bound config input, protected receipt binding, old-ready generation retention during failed rollout, bounded cleanup and separate Published/Activated/RuntimeReady states |
| Durable HTTP source cache | Preserve P0 SSRF/redirect/auth rules on retries, private bounded cache, validator/auth identity separation, explicit expiry and no failure-to-empty conversion |
| Multiple target profiles | Independent target authorization and scheduler budgets; adding one target grants no rights to another and cannot create an unbounded inventory/profile product |
| EgressPolicy routing | Explicit direct/block/final behavior, ordered rules, no empty-pool direct fallback, exact engine capability rejection and no subscription-supplied rules/hooks |
| Metrics and explanations | Bounded enum labels, no endpoint/resource/URL labels, authorized opt-in decision detail, deterministic truncation and credential canaries across text/JSON/status |

Managed runtime readiness can prove only that the exact process generation is ready
on its proxy listener. It is not proof of arbitrary destination reachability,
application success or high availability. A single replica plus PDB would not change
that limitation; runtime replicas and operator HA are outside the committed P1 path.

Redirect policy covers every hop by rejecting redirects. Internal endpoint/target use
is legitimate, so private-network exceptions are independent explicit trusted intent.
The M4 engine receives endpoint and target literals after local resolution, preventing
remote proxy resolution from bypassing these checks. DNS answers can change after an
observation, and deployment egress policy must still enforce an independent boundary.

## Sensitive data handling

Treat subscription URLs including paths/query strings, endpoint URIs, passwords,
UUID authentication values, private keys, Secret contents, HTTP headers, engine
configuration, and source caches as secret-bearing by default. Display aliases and
tags are untrusted strings and may themselves contain secrets/control characters.
Prefer safe application-generated IDs in diagnostics; bound and sanitize displayed
metadata. Never log the whole model with `%+v` or dump upstream response bodies.

Hashes/digests can permit guessing attacks on low-entropy credentials. Do not equate
hashing, Base64, encryption-at-rest, and redaction. Identity design must decide
which fingerprints stay private. Metric labels never contain credentials, endpoint
URIs, per-endpoint fingerprints, or full destination URLs.

Go memory does not offer a general guarantee of credential zeroization. Avoid
unnecessary copies/long-lived caches, but do not claim memory erasure. At-rest
encryption/key management is a separate deployment/design question. File modes,
Kubernetes Secrets, and redacted logs do not protect against privileged host access.

## Publication and least privilege

The [publication design](../designs/policy-rendering-publication.md) owns invalid-output
prevention and crash recovery. Never use public ConfigMaps for secret-bearing
artifacts. Stdout export is sensitive and opt-in; CI must not capture real configs.
Retained LKG generations and database/volume backups require the same protection
as current output. Validate cleanup and deletion behavior before deployment.

The M6 operator container is non-root, drops all capabilities, uses a read-only
root filesystem with explicit state/temp volumes, and exposes only required ports.
No NET_ADMIN, host networking, or privileged container is justified by P0.
Source and validator binaries must not execute arbitrary hooks from subscriptions.

## Security review triggers and residual risks

Update this model alongside new network fetch paths, parsers, engine invocations,
secret providers, credential-bearing persistence, outputs, native escape hatches,
CRD references, or RBAC changes. Add tests at the new boundary, not just a checklist.

Identity fingerprint privacy is resolved by [ADR 0006](../decisions/0006-versioned-endpoint-identity.md):
public IDs exclude credentials and private connection revisions remain sensitive.
Q3/Q4 probe and history choices are resolved by
[ADR 0009](../decisions/0009-bounded-probes-and-sqlite-evidence.md), and Q5 selection
by [ADR 0010](../decisions/0010-deterministic-adaptive-selection.md). Secret publication
and operator state/permissions/deletion are resolved by
[ADR 0011](../decisions/0011-namespaced-byo-operator.md). Runtime activation/rollback
remains under Q7. M3 validator and file recovery
choices are resolved by [ADR 0008](../decisions/0008-engine-artifacts-and-file-publication.md). All open
items are recorded in the [decision queue](../decisions/open-questions.md).

Release redistribution and supply-chain boundaries are resolved by
[ADR 0012](../decisions/0012-release-distribution-and-provenance.md). Signatures and
attestations prove which workflow identity produced bytes; they do not prove the
source, dependencies, vulnerability database, GitHub or Sigstore were benign.
Generated SBOM/license detection can be incomplete. GitHub private reporting and
protected-environment settings remain externally configured controls that must be
checked before publication.

Configuration validation cannot prove an endpoint is trustworthy, a destination
will remain available, or a runtime actually loaded the artifact. Operators remain
responsible for consent to probe targets, network authorization, engine hardening,
and deployment-level secret protection. Do not present planned controls as completed.
