# ADR 0015: Standalone control plane, runtime boundary, and engine packaging

Date: 2026-09-23. Status: accepted future architecture.

## Context

EgressFox has a Kubernetes-independent Go core and a working Kubernetes operator,
but no user-facing product CLI or standalone daemon. The M7 managed Gateway starts
an engine executable directly from the release image. The release image currently
contains `egressfox-operator`, `egressfox-healthcheck`, a Mihomo binary named
`mihomo`, and a sing-box-derived binary named `egressfox-engine-s`. These are
implemented facts; a standalone executable and common runtime wrapper do not yet
exist.

The product direction needs standalone Linux/VM/container operation without
duplicating endpoint, observation, selection, rendering or reconciliation logic.
It also needs a clear boundary between control-plane decisions and the process that
starts an engine. Existing redistribution, licensing and supply-chain guarantees
are authoritative in [ADR 0012](0012-release-distribution-and-provenance.md).

## Decision

EgressFox is an adaptive egress control plane with Kubernetes and standalone as
intended deployment frontends over one shared Go core. Kubernetes is not a product
dependency. The core decides which validated desired configuration should exist;
Mihomo or sing-box executes routing and forwards traffic. EgressFox may model
bounded routing intent and render it, but it never routes live packets/connections,
implements proxy protocols, or moves adaptive selection into an engine.

### Process roles

| Name | Accepted future role | Current implementation |
| --- | --- | --- |
| `egressfox` | Go user-facing one-shot CLI and long-running standalone control plane, reusing the shared core | Not implemented |
| `egressfox-operator` | Kubernetes control-plane frontend | Implemented at `cmd/operator` |
| `egressfox-runtime` | Thin common engine execution and local lifecycle adapter; not a control plane | Not implemented; M7 starts engines directly |
| `egressfox-engine-m` | Private executable name for the EgressFox-managed Mihomo build | Future name; current OCI path/name is `mihomo` |
| `egressfox-engine-s` | Private executable name for the EgressFox-managed sing-box-derived build | Current OCI executable name |

Public configuration selects logical engine names `mihomo` or `sing-box`. The
private `engine-m`/`engine-s` executable names are packaging details, not public
engine choices. Future command names and exact `engine:` spelling remain a CLI/API
design gate.

`egressfox-runtime` may select and locate an approved bundled engine, report
runtime/build identity, perform safe pre-exec checks, invoke exact native
configuration validation, launch/exec the engine, normalize startup failures,
preserve signal behavior, and participate in local readiness. It must not own
sources, inventory, endpoint admission or identity, observations/history, health
scoring, selection, policy, candidate replacement, routing decisions, failover,
traffic interception, protocols, or external publication. It exposes no network
control API and does not require a permanent supervisor merely to justify the
boundary. Exact CLI and lifecycle mechanics remain open.

### Standalone capability and distribution

The future Go `egressfox` supports both one-shot operations and a long-running
`run` control plane. Conceptual one-shot capabilities are inspect, generate, probe,
validate and version; those names are examples, not a frozen CLI. Inspect and
render-only generation need no engine. Real through-endpoint probes and native
validation require the exact supported engine. Probing endpoint reachability and
validating native configuration are independent capabilities. Stateless generation
must not invent persisted health evidence; adaptive history requires real state or
explicit non-historical semantics. Long-running source refresh, probes, history,
selection, render, validation, publication and managed activation belong to the
shared EgressFox control plane.

The recommended future standalone UX is one full `egressfox` executable per
supported platform, initially the release platform set. It may embed the release
payloads for `egressfox-runtime` and both managed engines, but those remain separate
executable programs and are never linked or run in-process. Payloads are
materialized lazily only for operations that need them. They come from the
signed/versioned EgressFox release; implicit engine downloads are prohibited. A
thin standalone executable may later serve render-only, distro or custom-runtime
use cases, but is not required for the first standalone distribution.

Materialization must verify expected payload identity, use a private user-owned
location, resist path/symlink substitution, write atomically with restrictive
permissions, reject unexpected existing content, support safe version coexistence,
and fail closed on mismatch. Cache location and cleanup policy remain open.

### OCI and canonical engine artifacts

The intended container model is one canonical signed/versioned OCI image containing
the supported process roles and one copy of each runtime/engine executable. The
operator, managed Gateway and standalone container run different processes from
that same image; this does not mean multiple roles share one process or container.
The OCI carries a thin `egressfox` plus `egressfox-operator`, `egressfox-runtime`,
private engine executables, required CA roots, licenses/notices and any helper that
remains necessary. It does not carry a full standalone binary with the same engines
embedded in addition to separate copies.

For each platform and release, future build/release tooling builds each canonical
runtime/engine executable once, verifies and identifies it, then uses those exact
bytes both in the OCI and as embedded standalone payloads. Independently rebuilding
nominally equivalent copies for the two packaging forms is not sufficient. The
identity must participate in release checksums, SBOMs, vulnerability scanning,
provenance, source archives and notices. Exact build mechanics are not decided here.

### Engine build and branding policy

Future EgressFox-managed builds should be production-capable and stay as close to
upstream production behavior as practical. Build inputs remain pinned, reviewed and
qualified. Downstream changes are limited to concrete legal branding, security
dependency, reproducibility, platform or narrowly justified integration needs.
Do not patch engine routing, add adaptive logic, restrict normal capabilities for
cosmetic size/branding, or maintain a deep unnecessary fork. A durable functional
patch requires explicit review and may require another ADR.

The current release manifest specifies Mihomo 1.19.31 with `with_gvisor` and no
branding overlay, and sing-box 1.14.1 with no build tags and a reviewed branding
overlay. The repository has not established that those profiles match every
upstream production package feature. That is a future research/qualification gap;
this ADR does not claim parity or change current build inputs. A full-capability
engine binary also does not mean EgressFox's renderer/API supports every upstream
feature: EgressFox's own compatibility contract stays bounded, explicit and tested.

Mihomo remains the factual upstream/compatibility identity and must not be renamed
for symmetry alone. Preserve the reviewed sing-box-derived branding/name change
required by its upstream name/association condition. Describe it factually as an
EgressFox-managed build derived from or compatible with sing-box, consistent with
the notices and review in ADR 0012.

Embedding separately executed GPL engine payloads in a standalone distribution does
not remove applicable license, notice or corresponding-source duties. EgressFox
remains Apache-2.0; engine derivatives retain their upstream obligations. ADR 0012
continues to own the release-security and compliance history.

## Alternatives rejected

- A Rust CLI/core would duplicate endpoint, source, probe, selection, renderer and
  reconciliation behavior rather than reuse the Go core.
- `egressfoxctl` would present the product as a client for a mandatory separate
  daemon, rather than the standalone control-plane process itself.
- Putting selection, routing or publication inside `egressfox-runtime` would create
  a second control plane and blur responsibility for live traffic.
- Linking engines as Go libraries or statically combining GPL engine code into the
  control-plane process would erase the intended execution boundary.
- Multiple independently versioned operator/runtime/engine images or duplicated
  embedded-plus-sidecar engines add release contracts without a demonstrated need.
- Artificially restricted engine editions and deep cosmetic forks add divergence
  without improving the bounded EgressFox renderer contract.

## Consequences

The current operator-only executable/image behavior remains the shipped contract
until later implementation and release work. Standalone CLI syntax, config schema,
precedence, output/exit-code schema, persisted-history opt-in, thin-build capability
reporting, runtime commands/signals, cache location/cleanup, and canonical build
mechanics remain open. P1 M8–M12 continue to establish shared primitives; the
post-P1 roadmap owns delivery sequencing for standalone and runtime work.
