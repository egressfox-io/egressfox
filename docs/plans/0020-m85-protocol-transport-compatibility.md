# M8.5 protocol and transport compatibility

Status: C1 complete; C2–C4 planned. Prepared on 2026-09-24
on `docs/m85-protocol-transport-roadmap`. C1 branch:
`codex/m85-c1-endpoint-capabilities`, based on `b8d5b50`.

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

## C1 record

The documentation branch established the scope without product changes. C1
inspected the repository's corresponding-source archives for both pinned commits
and the exact build flags in `release/manifest.json`. The
[engine matrix](../designs/engine-protocol-compatibility.md) is the sole
combination-status authority. [ADR 0021](../decisions/0021-version-three-connection-semantics-and-capabilities.md)
resolves v3 identity, private revision, domain validity and the exact-profile
capability gate; earlier identities and SQLite state need no migration.

The current sing-box derivative build has no optional tags. Its pinned source
uses `!with_quic` stubs for Hysteria2 and `!with_utls` stubs for Reality/uTLS.
[Q24](../decisions/open-questions.md) gates a separately reviewed build revision
before C3/C4 can claim those features. XHTTP exists in pinned Mihomo VLESS code
but is absent from pinned sing-box's transport union; C3 must preserve this
engine-specific difference. The existing URI/JSON parser and refresh/cache
pipeline are unchanged.

### C1 exit evidence

- [x] Pinned versions, build tags, source protocol/transport registration and
  real subscription formats inspected; one version-specific matrix recorded.
- [x] Typed protocol, transport, security and credential foundations, v3 identity,
  existing v1/v2 preservation and no schema migration.
- [x] Shared exact-profile capability check before rendering and probing; newly
  modeled forms fail explicitly without health observations or downgrades.
- [x] Existing-path and C1 regression checks, native traffic, full repository,
  documentation and vulnerability scan passed; final branch diff reviewed.

The v1/v2 golden IDs and protected revision bytes were captured from the clean
`b8d5b50` baseline and remain exact after C1, including VLESS and Trojan TCP/WS,
VMess WS and Shadowsocks TCP. The extended constructor also canonicalizes a
legacy form back to its existing identity. Synthetic v3 tests cover semantic
distinction, credential/Reality short-ID/obfuscation rotation, deterministic
deduplication, provenance, invalid combinations and redacted diagnostics.

Validation on 2026-09-24: `make fmt`, `make check` (full race suite, build,
offline documentation, Helm lint/template, release checks and whitespace),
`make docs` and `git diff --check` passed. `make vuln` reported zero reachable
vulnerabilities; it found one vulnerable imported package path that EgressFox
does not call. A five-second focused fuzz run completed 279,739 executions
without failure. The existing exact-profile native checks and controlled
through-engine traffic were rerun with Mihomo 1.19.31 and a release-equivalent
no-tag sing-box 1.14.1 derivative from the pinned source; they do not constitute
traffic evidence for any C2–C4 form. The no-tag sing-box executable rejected
synthetic Hysteria2 and Reality configurations at native check with its
`with_quic` and `with_utls` stub errors, respectively. No CRD, SQL, release
manifest or source-cache change was needed. Envtest/kind qualification was not
rerun because C1 changes no Kubernetes API or runtime integration.

## Next slice after C1

**C2 only** extends bounded input and full through-engine support for the
qualified VLESS, VMess, Trojan, Shadowsocks, SOCKS5 and HTTP/HTTPS proxy forms.
It must not implicitly begin C3 transports/Reality or C4 QUIC. Each new claim
needs synthetic fixtures, identity/admission/security tests, both applicable
renderers, exact-profile native checks, through-engine probes and managed traffic.
The C3/C4 contracts and explicit engine-specific gates remain in the
[matrix](../designs/engine-protocol-compatibility.md).
