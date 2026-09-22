# Runtime and standalone design

Status: accepted future architecture; standalone product CLI and runtime wrapper are
not implemented. [ADR 0015](../decisions/0015-standalone-runtime-and-engine-packaging.md)
owns the durable process and packaging decisions. [ADR 0012](../decisions/0012-release-distribution-and-provenance.md)
continues to own release compliance and supply-chain controls.

## Product and process boundary

EgressFox is an adaptive egress control plane. Kubernetes and standalone are
intended frontends over one Kubernetes-independent Go core. EgressFox decides the
desired validated configuration; Mihomo or sing-box executes routing and forwards
traffic. EgressFox may model bounded routing intent and render engine-native rules,
but it does not implement protocols, route individual live connections, or perform
adaptive choices inside the runtime.

| Process | Responsibility | State |
| --- | --- | --- |
| `egressfox` | Future Go one-shot CLI and long-running standalone control plane, composing shared core/use cases | Not implemented |
| `egressfox-operator` | Kubernetes API, scheduling, ownership, publication and managed-activation frontend | Implemented |
| `egressfox-runtime` | Future narrow process selection, exact validation/launch, identity, signals and local lifecycle/readiness adapter | Not implemented; M7 starts engines directly |
| `egressfox-engine-m` | Future private Mihomo executable name | Current binary is named `mihomo` |
| `egressfox-engine-s` | Private sing-box-derived executable name | Already used in the OCI image |

Users choose logical engines `mihomo` and `sing-box`. Private executable names do
not leak into public configuration. Mihomo remains the factual compatibility and
source identity. The sing-box-derived build keeps the reviewed branding required by
the upstream name/association condition. This is engineering guidance; see ADR
0012 and release notices for the reviewed legal/compliance record.

The runtime receives a local, already-materialized configuration file and a
logical engine profile. It does not watch Kubernetes, call the API server to fetch
configuration, contact Vault/S3, resolve EgressGateway/ProxyPool/EgressOutput, or
publish external output. It does not own source acquisition/refresh, inventory,
endpoint identity/admission, observations/history, scoring, selection, policy,
composition, candidate replacement, failover, or route selection. It has no network
control API and need not remain as a supervisor after safely starting the selected
engine.

The future runtime may locate the exact managed executable, report its identity,
check expected inputs/permissions, invoke the exact native validator, launch or exec
the engine, map logical profile to private binary, normalize startup errors, preserve
signals and provide local lifecycle/readiness hooks. Exact commands and whether it
execs or supervises are open implementation questions.

## One shared Go core

Core packages and use cases remain shared across frontends: `source`, `endpoint`,
`observation`, `probe`, `state`, `selection`, `policy`, engine rendering/profile
definitions, `artifact`, `publish` and `reconcile`. The Go CLI adds flags and
dependency/process wiring only. Kubernetes API types and client dependencies remain
in the Kubernetes adapter. Do not build a Rust copy of the control plane, an RPC
mesh between internal stages, or a public SDK just to support the product CLI.

M1–M5 already implement the Kubernetes-independent core and standalone
reconciliation use case; this does not mean a polished standalone CLI/daemon ships.
M6/M7 provide the Kubernetes frontend. Future standalone process wiring is added
only with its first real consumer, not as empty code directories.

## One-shot CLI capability semantics

The following names are explanatory examples, not frozen public syntax or a promise
that commands exist:

| Capability | Semantic operation | Engine/state requirement |
| --- | --- | --- |
| `inspect` | Acquire, parse, normalize, deduplicate and report a bounded inventory | No persistent daemon, engine, probes or final config; redact secrets |
| `generate` | Build Mihomo YAML or sing-box JSON from supported inputs, then optionally validate/publish | No Kubernetes or daemon; probing is optional; selection cannot invent history |
| `probe` | Run the real bounded through-endpoint check for an endpoint/profile/target | Compatible engine required; a TCP port check is not equivalent |
| `validate` | Validate existing or newly rendered config with the exact supported native engine | Selected engine required; do not reimplement its complete parser |
| `version` | Report EgressFox, runtime and supported engine profile identities | No engine extraction is required merely to report control-plane identity |
| `run` | Long-running standalone control-plane operation | Shared refresh, history, selection, publication and activation semantics |

Probing and native validation answer different questions. A run/generate invocation
may render without probes, probe then render/validate, probe without producing a
final artifact, natively validate without endpoint probes, validate an existing
config, or perform render-only generation. Exact flags are deliberately open.

Stateless generation must not fabricate historical evidence. An adaptive operation
that requires persisted state either loads a real configured history source or
selects an explicit mode whose semantics do not pretend history exists. The exact
history-loading and cold-start contract is open. One-shot output is secret-bearing:
stdout must be an explicit sensitive export channel, with diagnostics on stderr and
redaction throughout text, JSON and errors.

## Long-running standalone operation

`egressfox run` is the future standalone control plane, not the runtime wrapper. It
reuses shared core behavior for source refresh, inventory, scheduled probes,
persisted history, freshness summaries, eligibility, adaptive selection,
residence/hysteresis/cooldown/recovery, deterministic rendering, native validation,
LKG, external publication and managed activation/replacement. It does not fork
these semantics into CLI-specific logic.

Standalone must follow the availability principle that the current healthy LKG is
not destroyed before a replacement is proved usable. Candidate start/ready/traffic
handoff is difficult on a bare host because listener ownership and routing between
generations do not come from Kubernetes Services. The high-level LKG requirement is
accepted; exact local listener handoff, process supervision, signal and drain
mechanics are open and must be tested on supported platforms.

## Packaging and canonical runtime bytes

### Full standalone executable

The recommended future non-container distribution is one full `egressfox`
executable per supported platform, initially the current release platform set
(linux/amd64 and linux/arm64). It may embed signed/versioned release payloads for
`egressfox-runtime`, `egressfox-engine-m` and `egressfox-engine-s`. The embedded
programs remain separate executables. Engine source is not linked into the Go
control-plane process and engine implementation does not run in-process.

Materialize only the payload required by a command: inspect and render-only
generation require none; native validation/probe require the selected engine; a
managed `run` requires runtime plus selected engine. Payloads come from the
versioned EgressFox release. There are no hidden first-run engine downloads.

Before execution, a future materializer must check expected internal identity,
content/version identity and file type; use a private per-user directory; resist
symlink, parent-path and replacement races; write and rename atomically; apply
restrictive executable permissions; reject unexpected pre-existing content; fail
closed on mismatch; and permit safe coexistence between EgressFox versions. Exact
cache path and cleanup/retention behavior remain open.

A thin executable without embedded engines may later suit inspection/render-only,
distro packaging or installations with compatible artifacts provided by an
administrator. It is optional and not the primary UX. An engine-dependent operation
fails explicitly when the compatible executable is unavailable; it never silently
downloads one.

### One canonical OCI image

The future OCI uses normal separate executables: thin `egressfox`,
`egressfox-operator`, `egressfox-runtime`, `egressfox-engine-m`,
`egressfox-engine-s`, required CA roots, licenses/notices and only helpers still
needed. The same immutable image digest can run the operator process, a managed
Gateway runtime process, or standalone `egressfox run` in different containers.
One image means one release artifact contract, not one process or a combined
operator-plus-Gateway container.

Do not put the full standalone binary with embedded engines into the OCI while also
copying the same engines separately. For each platform and release, build and
identify each runtime/engine artifact once. Copy those exact bytes into the OCI and
embed those exact bytes in the standalone-full binary. Equal versions or checksums
of two independent builds are not a substitute for byte identity. Integrate
identity with the manifest, checksums, SBOM, scan, source provenance, engine
corresponding-source archives and attestations. Do not freeze build-tag mechanics.

This is accepted future packaging direction, not today's release procedure. The
current image and release artifacts remain described by
[the release guide](../operations/releasing.md).

## Engine build policy

Future engine builds should preserve production-capable upstream behavior with
pinned source, reproducible toolchains, reviewed security dependency overrides,
SBOM, vulnerability scanning, provenance and version qualification. Downstream
changes need concrete legal branding, security, reproducibility, platform or narrow
integration reasons. Reject cosmetic feature removal, routing changes, EgressFox
selection inside engines and deep unnecessary forks. Research the current
`with_gvisor` Mihomo tag and sing-box default/overlay against their upstream
production packaging in a future version-bound build review; the current repository
does not prove complete feature parity. Renderer/API support remains narrower and
explicitly versioned even if the binaries are full-capability.

## Compliance and release boundary

EgressFox remains Apache-2.0. Engine derivatives remain separate executable
programs under their upstream GPL obligations. Embedding their executable payloads
in a standalone file does not remove notices or corresponding-source duties. Future
standalone releases must identify included engine builds and preserve the source,
license, SBOM, scanning, signing, provenance and protected-publication guarantees
owned by ADR 0012. Do not invent a licenses or extraction command in advance.

## Open implementation questions

- Exact command names, flags, standalone YAML schema, config/environment/CLI
  precedence, output JSON version, exit codes and remote daemon-control API.
- Which one-shot modes may load history, how stateless selection is named, and how
  thin builds report available capabilities.
- Runtime `run`/`validate`/`version` command syntax, exec/supervisor choice, process
  signal semantics and local readiness contract.
- Materialization cache path, identity format, cleanup/retention policy and
  platform-specific atomic replacement behavior.
- Canonical artifact build/embedding mechanics and future standalone release
  qualification matrix.
- Whether a future TUI or remote client is justified; no second core language or
  TUI architecture is selected here.

## Related authority

- [Endpoint and source semantics](endpoints-and-sources.md)
- [Observation, history and selection](observations-and-selection.md)
- [Artifact publication and composition](artifact-publication-and-composition.md)
- [Policy, renderers and current publication](policy-rendering-publication.md)
- [Kubernetes API and current runtime](kubernetes.md)
- [Threat model](../security/threat-model.md)
- [P1 and post-P1 roadmap](../roadmap/p1.md)
