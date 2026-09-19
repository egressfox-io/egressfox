# EgressFox agent contract

EgressFox is a Go control plane that derives desired Mihomo/sing-box configuration.
M1 provides endpoint identity, and M2 provides bounded source acquisition, strict
URI ingestion and transactional snapshots. There is still no product CLI, probe,
renderer, publisher, CRD, or controller.
Read the [documentation map](docs/README.md),
[architecture](docs/architecture.md), and [current roadmap](docs/roadmap/README.md)
before implementation. Planned features are not existing behavior.

## Non-negotiable boundaries

- EgressFox decides desired configuration; engines proxy, tunnel, route, and balance
  connections. Do not implement a proxy protocol or datapath here.
- Core endpoint/policy/selection logic has no Kubernetes imports or concepts.
  Engine behavior belongs in version-aware adapters/renderers.
- Reconciliation is idempotent. Obsolete work must not overwrite newer desired state.
- Never log credentials, full subscription/endpoint URIs, or generated configuration;
  errors, metrics, status, Events, fixtures, and retained output need the same care.
- Invalid generated configuration never replaces last-known-good output.
  Publication success does not prove runtime activation.
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

## Validation

Use the Go version in [go.mod](go.mod), Git, Make, and a C compiler for race tests.
Run `make fmt` after Go edits; `make check` runs formatting, vet, race tests, build,
offline documentation links/anchors, and whitespace checks. Run `make vuln` for
the online pinned vulnerability scan. `make help` lists available commands.

These checks cover repository tooling and the M1/M2 endpoint and source domains.
Add relevant layers from the [test strategy](docs/development/testing.md) as behavior appears:
parser fuzzing, renderer goldens/native validation, publication failure tests,
SQLite integration, envtest, and real-cluster/traffic tests. Do not claim
unavailable checks passed.
Complete the workflow's definition of done before handoff. Completed execution
records are the [endpoint plan](docs/plans/0002-endpoint-identity.md) and
[source plan](docs/plans/0003-source-inventory.md); the roadmap identifies M3 as
the next milestone, gated by Q6 and the file part of Q7.
