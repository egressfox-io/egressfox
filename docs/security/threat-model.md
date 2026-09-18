# Threat model

Status: design requirements, **not implemented runtime protections**. Bootstrap
provides reviewed repository tooling, read-only CI permissions, pinned Actions,
and a vulnerability-check command. There is no runtime to secure or supported
production release. Reporting guidance is in [SECURITY.md](../../SECURITY.md).

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

Redirect policy must cover every hop. Internal-source use is legitimate, so exceptions
must be explicit deployment/user intent with scoped permissions rather than a
blanket relaxation. A proxy may resolve a hostname remotely; local DNS inspection
alone cannot prove the remote destination address. Q3 must choose what is enforceable
and document residual risk before shipping through-endpoint probes.

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

Future containers should be non-root, drop unnecessary capabilities, use a read-only
root filesystem with explicit state/temp volumes, and expose only required ports.
No NET_ADMIN, host networking, or privileged container is justified by P0.
Source and validator binaries must not execute arbitrary hooks from subscriptions.

## Security review triggers and residual risks

Update this model alongside new network fetch paths, parsers, engine invocations,
secret providers, credential-bearing persistence, outputs, native escape hatches,
CRD references, or RBAC changes. Add tests at the new boundary, not just a checklist.

Open issues include identity fingerprint privacy (Q1), network policy enforcement
through remote engines (Q3), backup/retention/SQLite driver choices (Q4), validator
isolation and version support (Q6), publication recovery (Q7), and operator
state/permissions/deletion (Q8–Q9). All are recorded in the
[decision queue](../decisions/open-questions.md).

Configuration validation cannot prove an endpoint is trustworthy, a destination
will remain available, or a runtime actually loaded the artifact. Operators remain
responsible for consent to probe targets, network authorization, engine hardening,
and deployment-level secret protection. Do not present planned controls as completed.
