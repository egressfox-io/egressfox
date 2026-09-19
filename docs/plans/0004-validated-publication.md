# M3: Validated engine artifacts and recoverable file publication

Status: in progress. Prepared: 2026-09-19. Started: 2026-09-19.
Branch/baseline: `feat/validated-publication` from `bd61322`.

## Objective and boundaries

Implement the first safe path from admitted inventory through a minimal common
gateway model, deterministic Mihomo and sing-box rendering, exact-profile native
validation and journaled local file publication. The owning design is
[policy, renderers and publication](../designs/policy-rendering-publication.md), with
durable choices in [ADR 0008](../decisions/0008-engine-artifacts-and-file-publication.md).

No probes, observations, SQLite, adaptive selection, daemon, Kubernetes API/Secret,
managed runtime, reload, activation acknowledgment or M4 behavior belongs here.

## Decisions and entry gates

ADR 0008 resolves Q6 and the file portion of Q7. M3 targets Mihomo v1.19.31 and
sing-box v1.14.1 with a loopback SOCKS listener, deterministic selector and explicit
final route. Exact-byte native validation is required before publication. Protected
journal/receipt state recovers target commits; one writer per target is a composition
contract. Artifact generations are confidential.

## Checkpoints

- [ ] Record Q6/Q7, compatibility profiles, validation and crash semantics.
- [ ] Add minimal common policy and deterministic Mihomo/sing-box renderers with
  explicit capability failures and reviewed golden files.
- [ ] Add candidate/validated artifact types and restricted pinned native validation.
- [ ] Add atomic file publication, no-op detection, protected LKG/previous state,
  journal recovery, cancellation and focused failure seams.
- [ ] Prove exact native validation and controlled proxy traffic with pinned binaries;
  keep ordinary tests offline.
- [ ] Complete architecture/security review, full validation, documentation and
  coherent commits; leave a clean branch without downloaded binaries or artifacts.

## Progress and evidence

Discovery reviewed repository state and history, AGENTS.md, roadmap/catalog,
architecture, source/endpoint and renderer/publication designs, threat model, all
ADRs and open gates, M1/M2 implementations and tests, workflow, CI and Makefile.
Current official Mihomo and sing-box configuration, transport, TLS, release and
validation documentation was reviewed on 2026-09-19. The standard library is enough
for sing-box JSON, artifacts, validation processes and file publication; deterministic
Mihomo YAML justifies one narrow serialization dependency.

## Resume and handoff

Next: update the owning design and decision queue, commit this decision checkpoint,
then implement the common model and renderers. Native binaries, generated configs and
traffic-smoke state must remain temporary and checksum-verified.
