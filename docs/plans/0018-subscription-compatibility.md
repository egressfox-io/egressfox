# Subscription compatibility completion

Status: complete. Date: 2026-09-24. Branch: `feat/subscription-compatibility`.
Baseline: `1e71bea` (completed M8).

## Objective and boundaries

Extend [completed M8](0017-resilient-http-sources.md) with deterministic HTTP
request profiles, explicit JSON subscription envelopes and a bounded set of
real-world endpoint variants that pass through the existing domain, render,
validation and activation path. Preserve M8 source cache, security and LKG.
M9 and generic native configuration ingestion are outside this plan.

## Confirmed baseline and gaps

M8 already has Secret-backed URL and Authorization, protected source cache,
conditional 304, retry, stale fallback, SSRF controls, source status and a
four-worker operator refresher. M2 accepts plain and Base64 URI lists, with
VLESS/Trojan over TCP or WebSocket (path only) and ordinary TLS. Both pinned
engines render that same subset and M7 tests controlled traffic. The API has no
client-profile fields or JSON format; Go supplies an implicit User-Agent; cache
identity lacks request headers. VMess, Shadowsocks, WebSocket Host, TLS ALPN and
Reality are absent throughout the endpoint and renderer path.

## Decisions and entry gates

- Extend ADR 0018 for HTTP profile and cache compatibility; add a focused endpoint
  ADR for the canonical identity and protocol subset before parser changes.
- Research primary upstream share-format and pinned-engine documentation before
  selecting JSON and protocol variants. Record the tested subset in one
  compatibility matrix.
- Keep existing explicit formats valid. Add explicit JSON and conservative Auto;
  format detection acts on one acquired body and never triggers a second request.
- Reality needs its complete model, renderer, probe and native traffic path and
  remains mandatory before v1.0; this task will not accept partial Reality fields.

## Checkpoints

- [x] Decision record, compatibility matrix and design gates.
- [x] Default, custom and clean HTTP profiles; CRD generation, true wire tests and
  profile-sensitive source cache identity.
- [x] Bounded JSON envelopes and endpoint record schemas with parser fixtures.
- [x] Coherent VMess/Shadowsocks and selected transport semantics through both
  engine renderers, native validation and controlled traffic tests.
- [x] Envtest, kind, full repository checks, documentation, diff review and clean
  atomic commits.

## Progress and evidence

The baseline working tree was clean; the task branch was created without altering
M8 history. Research sources and exact selected subset will be linked from the
compatibility matrix. The additional user requirement for arrays of complete
Xray/V2Ray client configurations is handled explicitly: only supported proxy
outbounds are extracted; Reality/Vision remains unsupported and strict admission
preserves LKG. Sing-box typed outbound arrays/configs form a second explicit JSON
schema. All examples and fixtures use synthetic credentials.

The focused source/operator/endpoint/engine suites, `make lint`, `make docs`,
`make helm-check`, `make release-validate`, `make test` (full race suite), and
`make test-envtest` passed. The new JSON fuzz target completed 621,888 executions
without a failure. `make vuln` found no called vulnerabilities; one imported
package finding was not called. Both checksum-verified pinned macOS engine binaries
passed native validation for new protocols. Controlled local traffic succeeded
through both engines for VMess TCP methods and WS/TLS with Host, all three selected
Shadowsocks AEAD methods, and VLESS/Trojan WS/TLS with Host.

`make e2e-kind` passed against Kubernetes 1.32. The controlled provider returned
an Xray client configuration as `text/plain`, with a service outbound, independent
DNS/inbound/routing sections and one supported VLESS outbound. Custom client
headers reached the provider; both managed engine profiles served authenticated
traffic. Conditional 304 kept generations stable; outage and operator restart used
the cache; a changed configuration rolled forward; malformed content and cache
expiry retained LKG. The run also passed existing BYO and managed regression paths,
Helm upgrade/uninstall, and removed its temporary cluster. Kubernetes 1.34 and 1.37
profiles were not run here.

`make check` initially stopped at its generated-file cleanliness gate because the
intentional new API and CRD diff was not yet committed. After committing the
implementation, rerun it on the clean branch and record the final result in the
commit/handoff. No remote publication was attempted.

## Handoff

The existing M8 refresh/cache behavior is preserved. The bounded compatibility
matrix documents the exact supported subset and safe unsupported classification,
including Reality/Vision. M9 target-aware profiles remains the next milestone;
this task did not implement it.
