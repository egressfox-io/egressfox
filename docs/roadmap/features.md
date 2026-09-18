# Feature catalog

This is the authoritative scope/priority catalog. **Every product feature below is
unimplemented at bootstrap.** Priorities express intent, not promises, API fields,
or permission to implement future work. [Milestones](README.md) group executable
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
| P0-08 | Standalone/CLI foundations and continuous reconciliation | M2–M5 |
| P0-09 | Structured diagnostics, Prometheus foundations and bounded decision evidence | M2–M6 |
| P0-10 | Proposed ProxyPool/EgressGateway APIs, operator reconciliation, status/Conditions, Secret references and Kubernetes Secret output | M6 |
| P0-11 | BYO/unmanaged runtime integration and Helm installation | M6 |

The first slices have explicit supported protocols/formats. No broad protocol list
is an implicit promise of support. P0 holds transactional source state through a
failed refresh but does not include the complete P1 HTTP caching/fallback system.

## P1 — Strong v1.0 experience

| ID | Capability group | Key dependency or caution |
| --- | --- | --- |
| P1-01 | Managed Mihomo and managed sing-box workloads | Runtime ownership, secure images/listeners, reload/activation and rollout semantics |
| P1-02 | EgressPolicy, multiple pools and explicit policy composition | Resolve gateway/policy precedence and alpha API evolution |
| P1-03 | Multiple probe profiles, destination-aware scoring and diversity-aware selection | Preserve health dimensions; define unknown/multi-source failure-domain attribution |
| P1-04 | Vault, credential-free ConfigMap and generic HTTP/webhook outputs | Each target needs idempotency, auth, acknowledgment and LKG semantics |
| P1-05 | Reload hooks and runtime activation reporting | Published is not activated; interruption/rollback handling |
| P1-06 | ServiceMonitor and Grafana dashboard | Bounded metrics and an actually supported monitoring contract |
| P1-07 | Source caching, ETag/Last-Modified, last-known-good source fallback | Correct source revision, auth scope, freshness, eviction and empty-response rules |
| P1-08 | Subscription quota/expiry metadata | Treat provider assertions as attributed data, not blindly trusted policy |
| P1-09 | Explainability CLI, dry-run and configuration diff | Compact P0 reasons; redact secret-bearing changes and secure detailed diagnostics |
| P1-10 | Renderer capability matrix/discovery, native escape hatch and base-config composition | Tested version-specific semantics and reserved-field ownership |
| P1-11 | Operator HA | Q9: supported state recovery and write fencing; not merely replica count |
| P1-12 | Strong runnable examples and operational documentation | Examples follow real supported features and test environments |

## P2 — Advanced product

| ID | Capability group | Constraint |
| --- | --- | --- |
| P2-01 | PostgreSQL | Concrete operational need and adapter contract, not speculative storage framework |
| P2-02 | Hierarchical/fallback pools | Cycle/precedence, failure and infeasible-selection semantics |
| P2-03 | Quota/cost-aware selection and custom scoring | Explainable policy, safe extension model, reproducibility |
| P2-04 | kubectl plugin and Web UI | Stable authenticated interfaces; no UI stack now |
| P2-05 | Advanced rollout strategies | Activation evidence and controlled traffic tests |
| P2-06 | Sidecar/transparent proxy experiments | Separate networking/security design; explicit proxy remains initial model |
| P2-07 | OpenTelemetry and additional outputs | Proven use case, cardinality/secrecy and target-specific guarantees |

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
- Protocol ecosystems: VLESS, VMess, Trojan, Shadowsocks, Hysteria/Hysteria2, TUIC,
  SOCKS, HTTP proxies, WireGuard where appropriate, and later supported protocols.
- Filtering: protocol, source, country, ASN, IPv4/IPv6, tags, names, allow/deny rules,
  latency and historical availability.
- Strategy alternatives: all, seeded random, lowest latency, highest availability,
  weighted and adaptive. Only an implemented subset becomes a supported enum.
- Policy vocabulary: pools/groups, destinations, domains, CIDRs, protocols, ports,
  rule sets, direct, fallback, URL-test-like behavior, balancing, final/default route.
- Output modes: complete configs, provider/proxy fragments, outbound fragments,
  sensitive stdout export, and future output adapters.
- CLI ideas: validate, fetch, nodes, probe, score, explain, render, diff, run; a
  Rust/Ratatui rich CLI/TUI is undecided and does not justify a workspace today.

The [product document](../product.md) owns the thesis purpose. Experimental static,
lowest-latency and adaptive comparisons are enabled by P0 testability, without
turning later research ideas into production commitments.
