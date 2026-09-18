# M2: Safe source-to-inventory pipeline

Status: in progress. Prepared: 2026-09-19. Started: 2026-09-19.
Branch/baseline: `feat/source-inventory` from `2f039cf`.

## Objective and boundaries

Implement the M2 roadmap slice from bounded inline or HTTP input through explicit
format handling, protocol-aware URI parsing, strict admission, source snapshots and
the M1 inventory. The owning design is
[endpoints and sources](../designs/endpoints-and-sources.md); the durable refresh and
security choices are in [ADR 0007](../decisions/0007-safe-source-snapshots.md).

No renderer, probe, history database, scoring, controller, CRD, publication path,
engine-native YAML/JSON adapter, scheduler, persistent source cache or product CLI
belongs in this plan.

## Decisions and entry gates

Q2 is resolved by ADR 0007 before implementation. M2 supports inline bytes and
bounded HTTP/HTTPS acquisition, plain URI lists and a single Base64 envelope, and
only the M1 VLESS/Trojan TCP/WebSocket and ordinary TLS subset. Explicit format
selection and conservative auto-detection are supported. Transactional complete
snapshots are the default; partial and empty replacement each require explicit
policy. The M1 identity and provenance contract in
[ADR 0006](../decisions/0006-versioned-endpoint-identity.md) remains unchanged.

## Checkpoints

- [ ] Record Q2, limits, source identity, format and refresh semantics in the ADR,
  source design and decision queue.
- [ ] Add bounded acquisition with context cancellation, HTTP status/redirect/dial
  policy, safe headers and redacted classified errors.
- [ ] Add strict URI-list/Base64 parsing, VLESS/Trojan normalization, deterministic
  diagnostics and configurable admission policy.
- [ ] Add immutable source snapshots, transactional replacement/removal and M1
  inventory construction preserving cross-source provenance.
- [ ] Prove bounds, classification, partial/empty behavior, refresh disappearance,
  determinism and leakage resistance with unit, integration and fuzz tests.
- [ ] Run full validation, complete documentation and architecture review, commit
  coherent checkpoints and leave a clean task branch.

## Progress and evidence

Repository discovery reviewed the Git state and history, AGENTS.md, roadmap,
architecture, source/endpoint design, threat model, testing/workflow guidance,
decision queue, M1 plan/ADR, the full endpoint package and its executable tests.
Official Xray VLESS sharing-link documentation and Trojan/Trojan-Go URI documentation
were reviewed on 2026-09-19. They confirm that duplicate fields are invalid and that
many apparently common URI parameters materially affect connectivity, so M2 must
recognize and reject options outside its typed domain rather than discard them.

No external Go dependency is planned: the standard library covers URL, Base64,
HTTP, DNS/dial policy and the chosen formats. YAML/JSON adapters are deferred rather
than adding a parser dependency without an admitted semantic mapping.

## Resume and handoff

Next: update the owning design and decision queue, commit the decision checkpoint,
then implement acquisition. The principal risks are URL parser edge cases, DNS/dial
policy bypass, bounded Base64 expansion and accidental secret retention in errors.
Tests must use synthetic input and local servers/fake resolution only.
