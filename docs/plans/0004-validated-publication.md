# M3: Validated engine artifacts and recoverable file publication

Status: complete. Prepared: 2026-09-19. Started: 2026-09-19. Completed: 2026-09-19.
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

- [x] Record Q6/Q7, compatibility profiles, validation and crash semantics.
- [x] Add minimal common policy and deterministic Mihomo/sing-box renderers with
  explicit capability failures and reviewed golden files.
- [x] Add candidate/validated artifact types and restricted pinned native validation.
- [x] Add atomic file publication, no-op detection, protected LKG/previous state,
  journal recovery, cancellation and focused failure seams.
- [x] Prove exact native validation and controlled proxy traffic with pinned binaries;
  keep ordinary tests offline.
- [x] Complete architecture/security review, full validation, documentation and
  coherent commits; leave a clean branch without downloaded binaries or artifacts.

## Progress and evidence

Discovery reviewed repository state and history, AGENTS.md, roadmap/catalog,
architecture, source/endpoint and renderer/publication designs, threat model, all
ADRs and open gates, M1/M2 implementations and tests, workflow, CI and Makefile.
Current official Mihomo and sing-box configuration, transport, TLS, release and
validation documentation was reviewed on 2026-09-19. The standard library is enough
for sing-box JSON, artifacts, validation processes and file publication; deterministic
Mihomo YAML justifies one narrow serialization dependency.

Implementation added a loopback SOCKS gateway policy, typed deterministic renderers,
secret-bearing candidate/validated artifact types, pinned native checkers and a
journaled local publisher. Publisher tests cover owned first/replacement/no-op
commits, `0600` modes, symlink/public-directory/unmanaged-target rejection, injected
rename and receipt-sync failures, rollback, restart recovery, temporary cleanup,
cancellation and in-process serialization. Ordinary tests remain offline.

Official Darwin arm64 Mihomo and sing-box assets were downloaded only to a private
temporary directory and matched the release-metadata SHA-256 values recorded in
ADR 0008. Both exact versions accepted their golden artifact. The opt-in integration
test additionally rendered, natively validated and atomically published the sing-box
artifact, then sent one controlled HTTP request through its SOCKS listener and a
local TLS Trojan endpoint. No binaries or runtime artifacts remain in the repository.

Validation completed on 2026-09-19:

- `make fmt` completed without further changes.
- `make check` passed, including `go vet`, race-enabled tests, build, documentation
  validation and both whitespace checks. The managed sandbox required loopback
  permission for the existing HTTP tests; the unrestricted local rerun passed.
- `make docs` passed independently.
- `make vuln` ran pinned `govulncheck` v1.8.0 and reported no vulnerabilities.
- `go test -race -count=10 ./internal/artifact ./internal/engine ./internal/publish`
  passed repeated deterministic, redaction and publication checks.
- With both verified binary environment variables set,
  `go test -race -count=1 -run
  'TestPinnedNativeValidation|TestSingBoxPublishedArtifactCarriesControlledTraffic'
  ./internal/engine` passed.
- `git diff --check` passed. Final staged checks and clean-tree/history inspection
  remain part of the documentation completion commit workflow.

Task commits before the final knowledge update are `eea6adf` (decision/plan),
`0c62e92` (policy, artifacts and renderers), `88ea644` (recoverable publication and
native smoke), and `c3c44f0` (exact version-token enforcement).

## Resume and handoff

M3 is complete on `feat/validated-publication`. Q3/Q4 and M4 are next; no probe,
history, SQLite, selection, daemon, Kubernetes or reload behavior was added. The
branch must remain clean and unmerged for review.
