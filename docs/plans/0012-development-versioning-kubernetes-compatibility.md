# Development versioning and Kubernetes compatibility

Status: complete. Date: 2026-09-22. Branch: `codex/versioning-k8s-compatibility`.
Baseline: `eae85dc`.

## Objective and boundaries

Establish an auditable development/release version contract and a tested Kubernetes
compatibility contract for the completed P0 and M7 product. The work centralizes
version and Kubernetes test inputs, makes local/CI validation selectable by
supported Kubernetes minor, and documents release identity, compatibility evidence
and lifecycle policy.

This is release/development infrastructure only. It does not add P1 behavior,
modify CRD semantics, or change the control-plane/data-plane boundary.

## Decisions and entry gates

- The initial planned development version is `v0.1.0-dev.1`; no Git tag is created
  by this task.
- A new ADR records exact release-tag forms, untagged build identity, Kubernetes
  support terminology, and the central manifest contract.
- Compatibility is claimed only after envtest and kind E2E run against each listed
  pinned Kubernetes profile. Kubernetes upstream support lifetime remains separate
  from EgressFox test coverage.
- The existing release manifest remains the authoritative machine-readable source
  for release inputs; it will gain version and Kubernetes profile metadata.

## Checkpoints

- [x] Record versioning/compatibility ADR, execution scope, and centralized metadata.
- [x] Parameterize build, envtest, kind, Helm and CI release validation by pinned
  Kubernetes profiles; add focused tests for manifest/version parsing.
- [x] Publish concise operator/developer documentation and release checklists.
- [x] Run validation, record real compatibility evidence, review, commit, and leave
  a clean branch.

## Progress and evidence

Discovery completed before changes:

- The repository was clean on `main` baseline `eae85dc`; no Git tags exist.
- P0 through M6 and M7 managed gateway are implemented. M8 is the next roadmap
  work and is expressly out of scope.
- `release/manifest.json` currently centralizes engine/release-tool provenance but
  has one Kubernetes `1.37.0` input. `Makefile`, envtest and kind scripts duplicate
  that fixed assumption.
- The current chart requires Kubernetes 1.37 and its metadata and dry-run tooling
  accept only alpha release tags. The operator already exposes linker-injected build
  information through `--version`.
- Official controller-runtime compatibility guidance maps v0.25 to Kubernetes
  libraries v0.37; this work will not downgrade application dependencies merely to
  exercise older clusters. Official controller-tools envtest and kind artifact
  provenance will be pinned in the manifest.

Implemented decisions and validation:

- Added ADR 0014 and central `release/manifest.json` metadata for planned
  `v0.1.0-dev.1`, Kubernetes 1.32/1.34/1.37 envtest SHA-512 values and digest-pinned
  kind images. `releasectl` rejects unknown profiles and invalid version tags.
- A tagged build accepts only stable, `dev.N`, `alpha.N`, or `beta.N` SemVer tags.
  Untagged Git/local builds report `+g<revision>`/`+local`, while release dry runs
  require an explicit tag. `make build` injects bounded version, revision and
  creation metadata into the existing operator `--version` output.
- `make check` passed, including generation stability, race tests, build, chart
  render, release-manifest validation and offline documentation links.
- `make vuln` passed: no reachable vulnerabilities; it reported one imported-package
  vulnerability with no detected call path.
- `make k8s-api-compat` passed for envtest and Helm on 1.32, 1.34 and 1.37. The
  explicit 1.37 run was repeated after the matrix check.
- `K8S_VERSION=1.32 make e2e-kind`, `K8S_VERSION=1.34 make e2e-kind`, and
  `K8S_VERSION=1.37 make e2e-kind` all passed. Each verified its pinned API-server
  minor before exercising Helm install/upgrade/uninstall, scoped RBAC, BYO traffic,
  both managed engines/authentication, activation/LKG, repair, restart and mode
  transitions. An initial run exposed whitespace-sensitive `/version` parsing;
  the script now normalizes JSON whitespace before matching the required minor.
- `VERSION=v0.1.0-dev.1 make release-dry-run` passed without skips. It built and
  compared both Linux operator builds, both source-built engines for amd64/arm64,
  generated/scanned SPDX SBOMs, packaged/rendered Helm for every qualification
  profile, created a multi-platform OCI archive, and verified `SHA256SUMS`. No
  registry, tag, image or release was published.

## Resume and handoff

No further work remains in this milestone. Future maintainers should add a profile
only through the manifest and complete `make k8s-compat`; 1.33, 1.35 and 1.36 remain
outside the independently tested contract. M8 resilient HTTP source refresh remains
the next product implementation milestone.

## Post-completion hardening: release integrity

Date: 2026-09-22. Same branch, before merge. Pre-merge fix-up of four release
integrity gaps found by review; no new product behavior and no change to the
Kubernetes contract.

- Published versions are immutable. The protected workflow previously created a
  release only when missing and then uploaded assets with `--clobber`, so a
  republished version silently replaced existing artifacts. It now refuses an
  existing release with an explicit error and uploads without clobbering; no
  delete, edit or recreate path exists. `releasectl release-guard` statically
  validates those properties and runs through `make release-validate`, which
  `make check` and the release validation job both invoke.
- Dirty development builds are marked. `internal/buildinfo` resolves one identity:
  a release tag on `HEAD` → `0.1.0-dev.1`; an untagged clean commit →
  `0.1.0-dev.1+g<commit>`; the same commit with non-ignored changes →
  `0.1.0-dev.1+g<commit>.dirty`. Ignored `dist/`, `.cache/` and `bin/` output never
  marks the tree dirty, and a tagged identity from a dirty tree fails.
- Release qualification requires a clean, exactly tagged commit.
  `hack/release-dry-run.sh` fails closed unless `VERSION` is the release tag on
  `HEAD` and `git status --porcelain` is empty. There is no dirty override; local
  `dist/` output stays repeatable because immutability applies to publication.
- Focused tests cover the clean, dirty, tagged, ambiguous-tag, ambiguous-version,
  dirty-qualification, and workflow-refusal cases, plus a mutation-driven check of
  the publication contract.
