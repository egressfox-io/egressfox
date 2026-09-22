# EgressFox agent contract

EgressFox is an adaptive egress control plane with intended standalone and
Kubernetes frontends over one shared Go core. It derives desired Mihomo/sing-box
configuration; engines execute traffic routing.
M1–M5 provide the Kubernetes-independent endpoint, source, render, probe, history,
selection and reconciliation pipeline. M6 adds a namespace-scoped BYO operator,
generated alpha CRDs, owned Secret publication and Helm delivery. M7 adds explicit
single-replica managed Mihomo/sing-box workloads, authenticated ClusterIP SOCKS and
exact-generation activation while preserving BYO. There is no product CLI, managed
source refresh, rich routing or HA topology. The P1 product sequence is designed in
`docs/roadmap/p1.md`; M8 is the next implementation milestone. After M12, continue
through the accepted post-P1 architecture in
`docs/roadmap/README.md` rather than duplicating those shared capabilities.
Read the [documentation map](docs/README.md),
[architecture](docs/architecture.md), and [current roadmap](docs/roadmap/README.md)
before implementation. Planned features are not existing behavior.

## Non-negotiable boundaries

- EgressFox decides desired configuration; engines proxy, tunnel, route, and balance
  connections. Do not implement a proxy protocol or datapath here.
- Intended frontends share one Go core: future standalone `egressfox` and current
  Kubernetes `egressfox-operator`. `egressfox-runtime` is execution/lifecycle only;
  selection and policy never belong there. `egressfox-engine-m/-s` are future
  private executable names; public engine choices remain Mihomo/sing-box. Do not
  duplicate the core in Rust.
- Core endpoint/policy/selection logic has no Kubernetes imports or concepts.
  Engine behavior belongs in version-aware adapters/renderers.
- Reconciliation is idempotent. Obsolete work must not overwrite newer desired state.
- Equivalent deterministic desired artifact means no new dataplane generation or
  restart. Reconciliation frequency and source/probe/status activity alone are not
  rollout triggers; missing/drifted targets may be repaired with the same generation.
- Managed activation and external publication are separate. Current M7 activation
  uses its internal generation Secret; future `EgressOutput` is optional external
  publication, not required for a managed Gateway. No reusable `EgressSink` is planned.
- ProxyPools remain leaf inventories, may contain multiple sources, and do not
  recursively include pools. Future candidate composition belongs to policy/selection.
- Never log credentials, full subscription/endpoint URIs, or generated configuration;
  errors, metrics, status, Events, fixtures, and retained output need the same care.
- Invalid generated configuration never replaces last-known-good output.
  Publication success does not prove runtime activation.
- Keep healthy active LKG until a replacement is ready; normal managed rollout must
  preserve Ready service capacity, stable Service identity and client credentials,
  and drain old connections best-effort without promising every session survives.
- Unsupported renderer semantics fail explicitly. Never silently weaken routing,
  TLS, selection constraints, or replace an empty pool with direct access.
- Public API/CRD and persistent-state changes require compatibility consideration.
  Architecture changes require design updates; behavior changes require tests.

## Find the authority

| Work | Read first |
| --- | --- |
| Source, format, endpoint, deduplication | [Endpoints and sources](docs/designs/endpoints-and-sources.md) |
| Probe, history, selection, metrics | [Observations and selection](docs/designs/observations-and-selection.md) |
| Policy, engine adapter, validation, output | [Renderers and publication](docs/designs/policy-rendering-publication.md) |
| API/operator/Helm | [Kubernetes design](docs/designs/kubernetes.md) |
| Secrets/network/parser/output boundaries | [Threat model](docs/security/threat-model.md) |
| Durable decisions or unsettled details | [ADRs](docs/decisions/README.md) and [decision queue](docs/decisions/open-questions.md) |
| Future process, runtime and package model | [Runtime and standalone](docs/designs/runtime-and-standalone.md) |
| Future artifact, publisher, composition and availability rules | [Artifact publication and composition](docs/designs/artifact-publication-and-composition.md) |

The [placement map](docs/architecture.md) guides future packages. Create them with
real behavior, not empty trees or speculative interfaces. `tools/checkdocs` is
repository tooling. There is one module; no public SDK or Rust workspace.

## Workflow and Git ownership

Follow the authoritative [development workflow](docs/development/workflow.md):

1. Inspect Git state/instructions and preserve pre-existing user changes.
2. Create a dedicated descriptive task branch; never do substantial work on main.
3. For cross-component, architectural, milestone, or multi-session work, create or
   update an [execution plan](docs/plans/README.md); resolve design gates before code.
4. Implement coherent checkpoints with tests and owning-document updates. Add or
   supersede an ADR when a durable boundary/decision changes.
5. Before every commit: inspect diff, run relevant checks, stage only intentional
   files, review staged diff, and commit. **Agents commit their own completed work.**
   Use `<emoji> <type>(<optional-scope>): <description>` as defined in the workflow.
6. Leave task work committed on a clean, reviewable branch, with evidence and plan
   current. Preserve/report unrelated pre-existing changes. Never automatically
   merge into main, push, destructively reset, force-push, or rewrite shared history.

Before completing a pull request or milestone, assess whether it introduces notable
user-facing changes. Add accurate entries under `CHANGELOG.md` → `Unreleased` in the
same branch when required; a release-preparation change may instead be included in
the reviewed version section before tagging. Otherwise record why no entry is needed.
Changelog review is part of the definition of done. Follow the authoritative policy in
[CONTRIBUTING.md](CONTRIBUTING.md).

When summarizing Conventional Commit subjects in the changelog or release notes,
preserve the subject's leading semantic emoji exactly once. Remove the
`type(scope):` prefix when turning a subject into a reader-facing entry, but do not
add another emoji or rewrite the commit. For example, `✨ feat(gateway): add managed
runtime` becomes `- ✨ Add managed runtime`. Keep the pull request title aligned with
its Conventional Commit subject so GitHub-generated release notes retain that
signal.

Release notes come from the reviewed `CHANGELOG.md` version section. Preserve
semantic emojis exactly once and keep remote publication fail-closed: check GitHub
Release and GHCR tag state before any write, reject partial publication and reruns,
and never replace published artifacts. Never push, tag, merge, publish or change
remote settings without explicit authorization. See the [release guide](docs/operations/releasing.md)
for the operational contract; security and validation gates above remain mandatory.

## Validation

Use the Go version in [go.mod](go.mod), Git, Make, and a C compiler for race tests.
Run `make fmt` after Go edits; `make check` runs formatting, vet, race tests, build,
offline documentation links/anchors, and whitespace checks. Run `make vuln` for
the online pinned vulnerability scan. `make help` lists available commands.

Release identity and Kubernetes qualification are centralized in
[`release/manifest.json`](release/manifest.json). Read the
[versioning](docs/operations/versioning.md) and
[Kubernetes compatibility](docs/operations/kubernetes-compatibility.md) contracts
before changing release tooling. The default Kubernetes profile is 1.32; the
release matrix is 1.32, 1.34 and 1.37 via `make k8s-compat`. Do not claim a profile
passed until its envtest and kind evidence is recorded.

These checks cover repository tooling and the M1–M6 endpoint, source, renderer,
artifact, publication, observation, probe, history, selection and operator domains.
Add relevant layers from the [test strategy](docs/development/testing.md) as behavior appears:
parser fuzzing, renderer goldens/native validation, publication failure tests,
SQLite integration, envtest, and real-cluster/traffic tests. Do not claim
unavailable checks passed.
Complete the workflow's definition of done before handoff. Completed execution
records are indexed in [execution plans](docs/plans/README.md); the
[P1 roadmap](docs/roadmap/p1.md) identifies M8 resilient HTTP source refresh as the
next bounded implementation milestone.
