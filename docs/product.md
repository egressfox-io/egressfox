# Product and scope

Status: product intent with P0 M1–M6 and P1 M7 implemented.
The [feature catalog](roadmap/features.md) owns priority assignments; the
[roadmap](roadmap/README.md) owns implementation order and completion status.

## Purpose

EgressFox is an open-source, Kubernetes-native control plane for reliable,
policy-driven proxy/VPN egress. Its project domain is `egressfox.io`.
It will discover, monitor, evaluate, and select external endpoints, then
continuously produce desired configuration for Mihomo and sing-box.

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
| Managing an explicitly requested engine process and observing exact activation | Loading and executing the engine-native configuration |
| Explaining configuration decisions | Serving proxy traffic and engine-local runtime behavior |

EgressFox may start an engine to perform through-endpoint probes. That is an
adapter to an existing data plane, not permission to implement a proxy stack.
Control-plane selection and engine-local selection must remain distinguishable.

## Initial product boundary

P0 is a sequence of completed implementation milestones. It ends with a standalone
reconciliation path and a Kubernetes operator for
user-managed runtimes, using both target renderers, historical adaptive selection,
file/Secret publication, and basic observability. Protocol and routing support
will be explicitly bounded and tested; P0 does not promise every upstream feature.

Standalone operation must remain possible without Kubernetes. A future command
such as `egressfox run --config config.yaml` expresses the intended experience;
there is no such executable or configuration schema today. Likewise, names such
as `validate`, `fetch`, `nodes`, `probe`, `score`, `explain`, `render`, and `diff`
are CLI direction, not existing commands or a frozen command contract.

The first Kubernetes experience publishes configuration for a runtime the user
operates (BYO/unmanaged). M7 additionally implements an explicit single-replica
managed workload and authenticated SOCKS5 ClusterIP Service. Applications still
opt in with proxy settings; EgressFox does not transparently attach workloads.

## P1 product direction

The authoritative [P1 roadmap](roadmap/p1.md) turns the completed BYO control plane
into a self-contained, namespace-scoped egress Service without changing which side
owns the data plane. M7 has added the managed authenticated SOCKS runtime and
exact-generation activation evidence. The remaining must-have path adds resilient
managed source refresh,
target-aware selection profiles, bounded routing policy, operational metrics and a
redacted explanation surface.

P1 intentionally chooses product completeness over feature count. BYO remains a
supported mode, `EgressPolicy` expresses engine-executed routing rather than workload
attachment, and readiness never becomes a claim that arbitrary destination traffic
works. Operator HA, transparent routing, cross-namespace references, Vault, generic
outputs, native deep merge, broad protocols and a Web UI remain later work.

## Non-goals

- A new proxy/VPN implementation, packet router, or connection balancer.
- Transparent interception, Cilium integration, admission-driven workload
  routing, eBPF, sidecar injection, or TProxy automation in P0 or the committed P1 path.
- A universal abstraction covering every feature of every engine.
- A multi-tenant or highly available distributed control plane in P0 or P1.
- A Rust workspace or a rich TUI in P0 or the committed P1 path.
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
