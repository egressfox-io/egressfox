# Release hardening and supply-chain readiness

Status: complete. Prepared/completed: 2026-09-21.
Branch/baseline: `chore/release-hardening` from `0a198fd`.

## Objective and boundaries

Turn the completed M1–M6 P0 implementation into a deliberate, repeatable first
alpha release path without publishing anything. Resolve Q12; centralize exact engine
artifacts; define licensing/source availability, versions and architectures; harden
the image and CI trust boundary; generate SBOMs, vulnerability evidence, checksums,
signatures and provenance; package Helm; and provide a non-publishing dry run and
acceptance checklist.

Managed engines, EgressPolicy, richer routing/protocols, Vault, cross-namespace
references, HA, Web UI and every other P1+ feature remain out of scope. The task
does not push commits/tags/images, create a GitHub release, change repository
settings, or use a live signing identity.

## Decisions and entry gates

Q12 research uses the exact Mihomo v1.19.31 and sing-box v1.14.1 repository tags,
license files and immutable release metadata. Both programs permit redistribution
under GPLv3 terms; sing-box uses GPL-3.0-or-later and additionally prohibits a
derivative from using its name or implying association without consent. Initial
scans found reachable high/critical findings in the official binaries, so release
images instead build reviewed derivatives from the pinned sources and fixed module
requirements. The sing-box derivative is branded `egressfox-engine-s` and disclaims
association. Releases carry licenses, notices and complete modified source including
resolved module files and conspicuous changes. EgressFox remains Apache-2.0. This is
an engineering compliance decision based on the cited text, not legal advice.

The initial release contract is SemVer prerelease tags, exact Mihomo 1.19.31 and
sing-box 1.14.1 profiles, Kubernetes 1.37.0, and linux/amd64 plus linux/arm64. One
release version feeds binary metadata, OCI labels/tags and Helm package/appVersion.
Immutable image digests remain the deployment identity. Release publication is a
separate trusted manually approved/tag-bound job; ordinary CI remains read-only.

Pinned release tools are Syft 1.52.0 (SPDX JSON), Grype 0.119.0, Cosign 3.1.3 and
Helm 4.3.0. Keyless GitHub OIDC signing and GitHub/Sigstore attestations avoid
repository-held private keys. No SLSA level is claimed. Exact decisions and evidence
are recorded in ADR 0012 and the release guide.

## Checkpoints

- [x] Reconstruct repository state and M0–M6 boundaries; inspect Git, build,
  dependencies, CI, image, Helm, generated artifacts and existing security tooling.
- [x] Research exact upstream licenses/releases and select a compliant redistribution
  model for Q12.
- [x] Add the engine/tool manifests, verification tests, notices/licenses and source
  availability contract.
- [x] Add coherent version metadata, hardened multi-architecture image inputs and
  Helm/version consistency checks.
- [x] Add dry-run release construction, SBOMs, scans, checksums and acceptance checks.
- [x] Add trusted release signing/provenance workflow with least privilege and no
  publication during validation.
- [x] Update security/reporting, threat model, README, operations, roadmap, ADRs and
  maintainer/user verification documentation.
- [x] Run all repository, native, Kubernetes and release validations available in
  this environment; record precise limitations; commit atomic checkpoints and leave
  the branch clean.

## Progress and evidence

Discovery found a clean local `main` at `0a198fd`, 41 commits ahead of
`origin/main`, with no tags. M6 directly downloads four official Linux engine assets
inside the Dockerfile; versions and SHA-256 values match GitHub release metadata but
are not centralized. The final Alpine image is digest-pinned, non-root, capability-
free and read-only-root compatible; it has no OCI labels, embedded third-party
notices, SBOM, image scan, release version injection or publishing workflow.

The exact upstream tag commits are Mihomo
`ab405bad5beeeac8b003bb01f60f134f6df54471` and sing-box
`1ac1a339cb1223e9c70eae14c44411c75033c02d`. Official release metadata supplies
SHA-256 digests for both linux/amd64 and linux/arm64 assets. The repository already
pins Go 1.27.1, controller-gen 0.22.0, envtest/Kubernetes 1.37.0, kind 0.33.0,
container bases and GitHub Actions; release-specific tools and version flow remain
to be implemented.

Docker is installed locally but its daemon was unavailable during discovery; Podman
is installed and will be evaluated for the image dry run. Helm 4.1.1 and Cosign are
installed, while Syft and Grype are not, so the dry-run tooling must bootstrap the
checksum-pinned versions without registry credentials.

ADR 0012 now resolves the repository-owned portion of Q12. The checked manifest
binds both compiled profiles to exact source archives, license files, build revisions,
module requirements, feature tags, branding overlays and reference official artifacts,
plus checksum-pinned Syft, Grype,
Cosign and Helm downloads for supported developer/CI hosts. `releasectl` validates
the contract and materializes only verified bounded artifacts. Unit tests cover the
repository manifest, raw/gzip/tar extraction, checksum failure and traversal input.

## Resume and handoff

The complete Docker Buildx dry run passed for linux/amd64 and linux/arm64. It produced
byte-identical repeated operator/engine builds, complete modified engine source,
SPDX JSON SBOMs, a Helm package, a multi-platform OCI archive and a verified sorted
SHA-256 manifest. Source-mode `govulncheck`, six artifact Grype reports and the final
image Grype report passed the high/critical gate. An initial final-image scan exposed
an Alpine zlib finding; the release image now uses `scratch`, contains no OS packages,
and the repeated final-image scan reports zero high/critical findings. The pinned
Alpine stage remains only to supply the CA bundle and license material.

Validation completed with `make fmt`, `make generate`, `make manifests`,
`make helm-check`, `make check`, `make test-envtest`, `make vuln`,
`VERSION=v0.1.0-alpha.1 make release-dry-run`, `make e2e-kind` and
`git diff --check`. The envtest controller suite, Kubernetes 1.37.0 kind traffic/RBAC/
restart/LKG/Helm lifecycle test and both supported architecture builds passed. The
repository scan reported no reachable Go vulnerabilities; it separately disclosed
one imported-module finding with no called vulnerable symbol. No live signing,
attestation, registry push, tag or release was performed.

External maintainer actions remain intentionally manual: enable GitHub private
vulnerability reporting, protect the `release` environment with required reviewers,
restrict release tag creation, and confirm the GHCR package visibility/retention
policy before the first publication. P1 remains unstarted.
