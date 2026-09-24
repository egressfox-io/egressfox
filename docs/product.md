# Product and scope

Status: product intent with P0 M1–M6 and P1 M7–M8 implemented; standalone CLI/runtime
and universal external publication are accepted future architecture, not shipped.
The [feature catalog](roadmap/features.md) owns priority assignments; the
[roadmap](roadmap/README.md) owns implementation order and completion status.

## Purpose

EgressFox is an open-source adaptive egress control plane for reliable,
policy-driven proxy/VPN egress. Its project domain is `egressfox.io`. Kubernetes is
one deployment frontend, not a product requirement. A standalone Linux/VM/container
frontend is an intended first-class model over the same Kubernetes-independent Go
core; its product CLI and daemon are not implemented today. The core discovers,
monitors, evaluates and selects external endpoints, then produces desired
configuration for Mihomo and sing-box.

Static endpoint lists decay. Providers remove endpoints, paths become slow,
destinations fail selectively, and apparently different endpoints share failure
domains. Operators should be able to describe acceptable egress and have a
reproducible explanation of how the chosen configuration follows that intent.

**EgressFox decides what configuration should exist. Mihomo or sing-box decides
how traffic flows through it.**

Intended uses include resilient third-party API access, redundancy across
providers, regional integration testing, geo-aware egress, centralized Kubernetes
egress, hybrid/multi-cloud networking, gateway failover, and latency-aware selection.
The product is not defined around jurisdiction-specific restrictions.

## Ownership of behavior

| EgressFox owns | Data-plane engine owns |
| --- | --- |
| Acquiring and normalizing endpoint descriptions | Implementing proxy and tunnel protocols |
| Collecting bounded, destination-aware observations | Forwarding application connections and packets |
| Maintaining history and selecting eligible endpoints | Executing routing rules for individual connections |
| Translating desired policy into engine configuration | Connection-level balancing and runtime failover groups |
| Validating and safely publishing desired generations | TUN, TProxy, and other packet handling |
| Deciding when an explicitly managed engine generation should activate | Loading and executing the engine-native configuration |
| Explaining configuration decisions | Serving proxy traffic and engine-local runtime behavior |

EgressFox may start an engine to perform through-endpoint probes. That is an
adapter to an existing data plane, not permission to implement a proxy stack.
Control-plane selection and engine-local selection must remain distinguishable.

## Initial product boundary

P0 is a sequence of completed implementation milestones. It ends with a
Kubernetes-independent reconciliation path and a Kubernetes operator for
user-managed runtimes, using both target renderers, historical adaptive selection,
file/Secret publication, and basic observability. The shared core can be composed
without Kubernetes, but there is no user-facing standalone `egressfox` executable,
CLI contract or standalone daemon yet. Protocol and routing support are explicitly
bounded and tested; P0 does not promise every upstream engine feature.

The accepted future standalone product executable is Go `egressfox`, reusing the
shared core for one-shot operations and long-running control-plane operation. A
command such as `egressfox run --config config.yaml` is only an illustration; there
is no such executable or configuration schema today. `egressfox-runtime` is a
separate future execution/lifecycle adapter, not another control plane. Likewise,
names such as `inspect`, `generate`, `probe`, `validate`, `version`, and `run` are
capability examples, not existing commands or a frozen CLI contract. Rust is not
the planned primary CLI/core implementation.

The first Kubernetes experience publishes configuration for a runtime the user
operates (BYO/unmanaged). M7 additionally implements an explicit single-replica
managed workload and authenticated SOCKS5 ClusterIP Service, currently starting
engine binaries directly without `egressfox-runtime`. Applications still opt in
with proxy settings; EgressFox does not transparently attach workloads.

## P1 product direction

The authoritative [P1 roadmap](roadmap/p1.md) turns the completed BYO control plane
into a self-contained, namespace-scoped egress Service without changing which side
owns the data plane. M7 added the managed authenticated SOCKS runtime and
exact-generation activation evidence. M8 added resilient managed HTTP source
refresh with a protected cache. The remaining must-have path adds
M8.5 practical proxy protocol/transport compatibility before target-aware
selection profiles, bounded routing policy, operational metrics and a redacted
explanation surface. M8.5 covers documented combinations found in subscriptions,
not every option or cross-product supported by either engine.

P1 intentionally chooses product completeness over feature count. BYO remains a
supported mode, `EgressPolicy` expresses engine-executed routing rather than workload
attachment, and readiness never becomes a claim that arbitrary destination traffic
works. Operator HA, transparent routing, cross-namespace references, future
Vault/S3/filesystem/stdout publisher adapters and `EgressOutput`, native deep merge,
protocols beyond the bounded M8.5 contract and a Web UI remain outside P1. The
post-P1 roadmap points to standalone, runtime and publication work without
duplicating M8–M12 primitives.

## Non-goals

- A new proxy/VPN implementation, packet router, or connection balancer.
- Transparent interception, Cilium integration, admission-driven workload
  routing, eBPF, sidecar injection, or TProxy automation in P0 or the committed P1 path.
- A universal abstraction covering every feature of every engine.
- A multi-tenant or highly available distributed control plane in P0 or P1.
- A second control-plane/core implementation in Rust, or a rich TUI in P0 or the
  committed P1 path.
- A thesis-specific production architecture or unmeasured claims of adaptation quality.

## What success should look like

An operator can identify why a selected endpoint is eligible for a destination,
see how fresh that evidence is, reproduce a decision from a bounded snapshot,
and detect a failed source or publication without leaking credentials. Small
latency fluctuations do not cause churn. Confirmed failure can trigger prompt
replacement once the configured detection criteria are met. Invalid generated
configuration never displaces the last-known-good artifact.

This does not promise uninterrupted traffic or instantaneous detection. BYO
publication still does not prove activation; managed activation proves only the
exact process/listener generation described in ADR 0013, not destination traffic.

## Research purpose

EgressFox also supports a master's thesis on adaptive management of outbound
traffic through changing external gateways. Compare static, lowest-latency, and
adaptive selection under the same controlled failures and workload. Measure
success rate, availability, p95/p99 latency, failover time, unnecessary switches,
and recovery under flapping. Production interfaces should enable experiments
without depending on the experiment harness. See [testing and experiments](development/testing.md).
