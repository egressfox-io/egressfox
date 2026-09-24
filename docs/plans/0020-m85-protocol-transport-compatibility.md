# M8.5 protocol and transport compatibility

Status: planned. Prepared on 2026-09-24 on
`docs/m85-protocol-transport-roadmap`. Baseline: `318d4fe` (M8 hardening complete).

The [P1 M8.5 roadmap](../roadmap/p1.md#m85--protocol-and-transport-compatibility)
owns the mandatory families, transport/security scope and milestone exit contract.
The [current compatibility matrix](../designs/subscription-compatibility.md) is
the implemented M8 baseline, not an M8.5 support claim. This plan orders future
implementation work; preparing it changes no product behavior.

## Dependencies and design gates

- Preserve ADRs [0006](../decisions/0006-versioned-endpoint-identity.md),
  [0008](../decisions/0008-engine-artifacts-and-file-publication.md) and
  [0019](../decisions/0019-subscription-endpoint-semantics.md) for existing
  records. Never reinterpret stored `ef1_`/`ef2_` identities or reuse observations
  across materially changed connection semantics.
- Resolve [Q21–Q23](../decisions/open-questions.md) with pinned-engine and real
  subscription evidence during C1 before changing canonical types, persistent
  identity, API fields or renderer capability claims. Add an ADR only for a
  durable decision actually made from that evidence.
- Keep source acquisition, admission, protected cache, provenance, SSRF policy,
  validated publication, active LKG and managed runtime activation on their
  existing paths. No subscription-to-runtime shortcut or engine-native deep merge.

## Ordered checkpoints

| Slice | Depends on | Implementation and evidence required |
| --- | --- | --- |
| C1 — Model and capabilities | Completed M8 | Research subscription variants and both pinned engine builds; decide typed endpoint/identity changes, state migration or invalidation, and an exact per-engine combination matrix. Prove existing IDs, revisions and observations remain safe with compatibility tests. |
| C2 — Main protocols | C1 accepted matrix/model | Extend ingestion through managed traffic for the qualified VLESS, VMess, Trojan, Shadowsocks, SOCKS5 and HTTP/HTTPS proxy forms. Include explicit unsupported and redaction tests. |
| C3 — Transports and Reality | C2 end-to-end baseline | Add only C1-qualified transport/security pairs, including Reality and XHTTP where valid. Prove complete field preservation, engine-specific rejection, native checks, through-engine probes and managed traffic. |
| C4 — Hysteria2 and QUIC | C3 capability and probe boundaries | Add typed QUIC/Hysteria2 parameters, network authorization, probe execution and rendering. Prove controlled QUIC traffic for each claimed engine profile and safe rejection elsewhere. |

Each slice needs its owning design update, synthetic adversarial fixtures, parser
and identity tests, renderer goldens, exact-profile native validation, controlled
probe and managed Gateway traffic. The final matrix must list unsupported
combinations. A parsed URI or native syntax check alone cannot close a slice.

## Documentation preparation and next task

This documentation branch establishes the mandatory scope and ordered gates only.
No C1–C4 implementation or engine capability claim is made here. The next task is
**C1 only**: inspect real subscription representations without committing real
credentials; verify the exact pinned Mihomo/sing-box source and build profiles;
produce a versioned, per-combination support/rejection matrix; decide the typed
model, logical/private identity migration and observation invalidation; then
implement those model and capability foundations with focused tests. C2 ingestion
and traffic support must wait for that gate.
