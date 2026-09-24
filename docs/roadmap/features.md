# Feature catalog

This is the authoritative scope/priority catalog. **Every product feature below is
unimplemented unless the roadmap marks it complete.** Priorities express intent,
not promises, API fields, or permission to implement future work.
[Milestones](README.md) group executable
work; designs define semantics. Update this catalog when scope changes deliberately.

## P0 — Core identity and first usable product

| ID | Capability group | Initial milestone |
| --- | --- | --- |
| P0-01 | Normalized endpoint model, deterministic identity, provenance and deduplication | M1 |
| P0-02 | Source acquisition, subscription parsing, semantic validation and static allow/deny filtering | M2 |
| P0-03 | Useful common routing abstraction, Mihomo and sing-box renderers, deterministic output, internal capability rejection and engine-version validation | M3 |
| P0-04 | File output, generation/content comparison, recoverable publication and last-known-good retention | M3 |
| P0-05 | Destination-aware probes with explicit execution path, budgets, freshness and outcomes | M4 |
| P0-06 | Historical state and initial SQLite persistence | M4 |
| P0-07 | Adaptive scoring, anti-flapping, hard eligibility and Top-N selection | M5 |
| P0-08 | Kubernetes-independent core/use-case foundations and continuous reconciliation | M2–M5 |
| P0-09 | Structured diagnostics, Prometheus foundations and bounded decision evidence | M2–M6 |
| P0-10 | Proposed ProxyPool/EgressGateway APIs, operator reconciliation, status/Conditions, Secret references and Kubernetes Secret output | M6 |
| P0-11 | BYO/unmanaged runtime integration and Helm installation | M6 |

The first slices have explicit supported protocols/formats. No broad protocol list
is an implicit promise of support. P0 holds transactional source state through a
failed refresh but does not include the complete P1 HTTP caching/fallback system.
Kubernetes-independent M1–M5 behavior does not mean a user-facing `egressfox` CLI
or standalone daemon is implemented.

## P1 — Self-contained namespace egress service

The [P1 roadmap](p1.md) is the authoritative implementation order. These must-have
capabilities form one product path rather than independent feature bids.

| ID | Capability group | Milestone |
| --- | --- | --- |
| P1-01 | Managed single-replica Mihomo/sing-box SOCKS workload and ClusterIP Service, with BYO preserved | M7 |
| P1-02 | Exact-generation activation and runtime-readiness evidence distinct from publication | M7 |
| P1-03 | Secret-referenced HTTP source refresh with validators, bounded retry and protected LKG cache | M8 |
| P1-09 | Practical VLESS, VMess, Trojan, Shadowsocks, SOCKS5, HTTP/HTTPS proxy and Hysteria2 compatibility, with applicable transports/security and exact-engine traffic qualification | M8.5 |
| P1-04 | Multiple named target/probe/selection profiles over one shared source inventory | M9 |
| P1-05 | One bounded EgressPolicy per gateway with ordered common routing and explicit final behavior | M10 |
| P1-06 | Low-cardinality lifecycle metrics, optional ServiceMonitor and supported dashboard/alerts | M11 |
| P1-07 | Go explainability CLI over a bounded authorized decision report | M12 |
| P1-08 | Managed-path examples, upgrade/recovery guidance and both-engine acceptance scenarios | M7–M12 |

Optional P1 candidates are not scheduled milestones: engine-specific live reload,
managed data-plane replicas/PDB/topology, source-provenance diversity, one further
demand-backed protocol slice beyond M8.5 and attributed subscription quota/expiry
display. They require a roadmap revision after the must-have path demonstrates
the need and fixes their API/security contract.

## P2 — Advanced product

| ID | Capability group | Constraint |
| --- | --- | --- |
| P2-01 | PostgreSQL | Concrete operational need and adapter contract, not speculative storage framework |
| P2-02 | Explicit fallback among leaf Pool/Profile candidate sets | Order, failure and infeasible-selection semantics; inventories remain non-recursive |
| P2-03 | Quota/cost-aware selection and custom scoring | Explainable policy, safe extension model, reproducibility |
| P2-04 | kubectl plugin, Web UI or a separately justified TUI | Stable authenticated interfaces; no UI stack now |
| P2-05 | Advanced rollout strategies | Activation evidence and controlled traffic tests |
| P2-06 | Sidecar/transparent proxy experiments | Separate networking/security design; explicit proxy remains initial model |
| P2-07 | OpenTelemetry and source/output adapters beyond the accepted post-P1 publisher family | Proven use case, secrecy and target-specific idempotency/acknowledgment guarantees |
| P2-08 | Operator HA and shared durable state | Real write fencing/recovery; no shared SQLite or replica-count shortcut |
| P2-09 | Cross-namespace references | Explicit grant/reference model and scoped Secret authorization |
| P2-10 | Engine-native composition and base-config escape hatch | Reserved-field ownership, safe merge and final native validation |

## P3 — Future platform exploration

| ID | Capability group | Constraint |
| --- | --- | --- |
| P3-01 | Cilium integration, automatic workload routing and admission integration | New trust/ownership boundaries; not implicit operator behavior |
| P3-02 | TProxy automation, eBPF/datapath integrations | Data plane remains delegated; privileges require explicit design |
| P3-03 | Multi-cluster/fleet management and multi-tenancy | State distribution, authorization/isolation, failure domains |
| P3-04 | Predictive selection, anomaly detection and dynamic gateway placement | Measurable benefit, reproducibility and robust baseline comparison |

## Extension inventory without a committed priority

Keep these in view when reviewing interfaces; decide a delivery milestone only
with a concrete use case:

- Sources: HTTP/HTTPS subscriptions, local files, inline config, environment-backed
  references, Kubernetes Secrets, and Vault. HTTP options include authentication,
  custom headers, User-Agent, retries and backoff; cache features are P1 above.
- Formats: URI lists, Base64 envelopes, Mihomo/Clash YAML and sing-box JSON.
- Reality with VLESS Vision and the other [M8.5 combinations](p1.md#m85--protocol-and-transport-compatibility)
  are mandatory before M9; the M8 compatibility extension still classifies them
  as unsupported until their full path is implemented. It never substitutes
  ordinary TLS or discards a public key, short ID or flow.
- Protocol ecosystems beyond the mandatory M8.5 families, including TUIC,
  Hysteria 1 and WireGuard where appropriate, remain unscheduled.
- Filtering: protocol, source, country, ASN, IPv4/IPv6, tags, names, allow/deny rules,
  latency and historical availability.
- Strategy alternatives: all, seeded random, lowest latency, highest availability,
  weighted and adaptive. Only an implemented subset becomes a supported enum.
- Policy vocabulary: pools/groups, destinations, domains, CIDRs, protocols, ports,
  rule sets, direct, fallback, URL-test-like behavior, balancing, final/default route.
- Output modes: complete configs, provider/proxy fragments, outbound fragments,
  sensitive stdout export, and future output adapters.
- CLI capability examples: inspect, generate, probe, validate, version and run;
  exact commands are future design. The primary product CLI is planned in Go over
  the shared core. A separate TUI or remote client is undecided and does not justify
  a second control-plane implementation or workspace.

The [product document](../product.md) owns the thesis purpose. Experimental static,
lowest-latency and adaptive comparisons are enabled by P0 testability, without
turning later research ideas into production commitments.
