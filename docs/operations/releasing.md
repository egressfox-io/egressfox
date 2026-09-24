# Release and verification guide

Status: first development release contract. No release is created by the commands in the
dry-run section. [ADR 0012](../decisions/0012-release-distribution-and-provenance.md)
owns redistribution and trust-boundary decisions; [ADR 0014](../decisions/0014-development-versioning-and-kubernetes-compatibility.md)
owns development versioning and Kubernetes qualification.

This guide describes the current operator, managed Gateway image and release files.
The future `egressfox` standalone distribution, `egressfox-runtime`, canonical
OCI/embedded engine byte sharing and shared publisher family are architecture
requirements only; see [runtime and standalone](../designs/runtime-and-standalone.md).

## Supported release contract

The initial public artifacts support exactly:

| Component | Supported and tested | Meaning |
| --- | --- | --- |
| Mihomo | 1.19.31 / `egressfox.mihomo/v1` | Exact native validator/profile; other versions are rejected even if they might be compatible |
| sing-box | 1.14.1 / `egressfox.sing-box/v3` (`with_quic,with_utls`, build revision 3) | Exact native validator/profile; other versions are rejected even if they might be compatible |
| Operator and managed Gateway image | linux/amd64, linux/arm64 | Operator, readiness helper and source-built engine derivatives are compiled for both architectures; the same signed image is the M7 managed runtime authority. Managed Pods start engine binaries directly. |
| Kubernetes | 1.32, 1.34, 1.37 | Pinned envtest and kind release-qualification profiles; not a promise for every older or newer minor |
| API | `egressfox.io/v1alpha1` | Alpha compatibility: review CRD diffs and release notes before every upgrade |

Allowed SemVer tags are `vX.Y.Z-dev.N`, `vX.Y.Z-alpha.N`, `vX.Y.Z-beta.N`, and
stable `vX.Y.Z`. The tag without `v` becomes the operator version, OCI version/tag,
packaged Helm `version`, `appVersion`, and default image tag. The full tagged commit
becomes binary/OCI revision metadata, and its commit timestamp is the build/OCI
creation input. Untagged builds carry only a local `+g<commit>` identity, with
`.dirty` appended when the source tree has non-ignored changes, and cannot be release
artifacts. Deploy by immutable image digest even though a human-readable tag exists.
See [versioning](versioning.md).

A published version is immutable: one version, one commit, one image digest, one
release file set. Publication checks both GitHub Release and GHCR tag state before
image push and never clobbers existing assets, so a change requires the next version
(`v0.1.0-dev.1` → `v0.1.0-dev.2` → `v0.1.0-alpha.1`). Neither the release workflow nor
a maintainer recovery step deletes or edits a published release. Publication is
strictly tag-bound and additionally requires a clean source tree and approval of the
protected `release` environment; `make release-validate` checks that the workflow
keeps enforcing this contract. GitHub Actions serializes release runs in one
repository-wide concurrency group. GHCR tags remain mutable to actors with package
write access; external tag protection and privileged human writes are outside the
workflow's atomicity guarantee.

If image publication succeeds but a later SBOM, signature, attestation or GitHub
Release step fails, the GHCR tag is reserved. A rerun fails closed even when no
GitHub Release exists. Preserve the partial artifacts for investigation, fix the
cause in a new reviewed commit and advance to a new version. Do not delete or
overwrite the old image, signature, provenance or release assets as routine recovery.

## Inputs and reproducibility

`go.mod`, digest-pinned container bases, [`release/manifest.json`](../../release/manifest.json),
and SHA-pinned Actions define the checked inputs. The manifest is the only authority
for engine/tool versions, URLs, architectures and SHA-256 values. Engine checksum
failure stops the build before extraction. The final `scratch` image has no shell,
package manager, curl, tar or OS packages. It runs as UID/GID 65532, has a state
volume and temporary volume, and remains compatible with a read-only root filesystem
and all capabilities dropped by the chart.

The operator and managed-readiness Go binaries are built reproducibly in the image;
the distributed operator binaries are built twice and compared during a dry run. This proves
byte equality for the two invocations in that environment, not universal
reproducibility across operating systems or future toolchains. OCI layer encoding,
upstream downloads, Helm gzip headers, SBOM timestamps and vulnerability-database
state can remain nondeterministic. The contract is reproducible inputs and commands;
no byte-for-byte image or SLSA-level claim is made.

Engine builds start from checksum-pinned source commits, apply the manifest's build
revision, feature tags, dependency requirements and branding overlay, then emit a
conspicuous change notice. Their resolved module files are included in the complete
modified source archives. The official upstream binary hashes remain manifest
evidence for compatibility research; those binaries are not release contents.

The Alpine CA bundle is checksum/package-version pinned in a builder stage and copied
into the final image, avoiding target-architecture package-manager execution. The
Alpine repository serving that exact package and upstream release hosting remain
external availability/trust boundaries. SBOMs describe observed output, not upstream
integrity.

## Non-publishing dry run

First generate and commit the changelog on a dedicated clean branch, then run release
qualification from that prepared commit:

```sh
VERSION=v0.1.0-dev.1 make release-prepare
VERSION=v0.1.0-dev.1 make release-prepared-check
VERSION=v0.1.0-dev.1 make release-dry-run
```

`release-prepare` is an explicit local command. It groups eligible emoji
Conventional Commits since the previous reachable SemVer release tag; for the first
release it starts at repository root. It excludes routine documentation, tests,
build, CI, dependency and internal refactor commits, removes duplicate descriptions,
preserves each leading emoji once, and commits only `CHANGELOG.md`. Review the
generated section and commit before making the tag. If another eligible commit lands,
rerun preparation. Ordinary CI and the public release workflow only check/read the
committed result; they never generate a preparation commit.

The requested version must be the planned release version that
`release/manifest.json` and the chart declare, and the working tree must contain no
non-ignored change; the committed changelog must match Git history. Otherwise the dry
run fails closed before building anything. It
does **not** require the Git tag: a dry run is pre-publication qualification, so the
tag is created afterwards, for publication only, and this repository tooling never
creates or pushes it. Ignored output under `dist/`, `.cache/` and `bin/` never counts
as a source change, so repeated dry runs stay repeatable. There is deliberately no
dirty override: commit or stash first.

Because qualification can run on an untagged commit, it reports the commit-derived
identity `0.1.0-dev.1+g<commit>`; the release workflow, which runs from the exact tag,
builds artifacts labeled exactly `0.1.0-dev.1`. Publication itself is strictly
tag-bound: it requires an existing exact Git tag on the selected commit, a clean
source tree rechecked in the privileged job, approval of the protected `release`
environment, and a version that has never been published.

This bootstraps checksum-pinned Helm 4.3.0, Syft 1.52.0, Grype 0.119.0 and Cosign
3.1.3 into the ignored `.cache` directory; validates the manifest/notices; builds
linux/amd64 and linux/arm64 binaries twice; verifies the engine source inputs; builds
both engine derivatives twice; creates modified source/license archives and SPDX JSON
SBOMs; packages/renders Helm; builds a
multi-platform OCI archive without registry credentials; scans release SBOMs/image,
including the managed readiness helper and both runtime engines;
and emits a sorted `dist/release/SHA256SUMS`. Nothing is uploaded, signed or pushed.

If no container service exists, use `RELEASE_SKIP_IMAGE=1` and record that image
construction/SBOM/scan remain incomplete. If the vulnerability database is
temporarily unavailable, `RELEASE_SKIP_VULN=1` records a skip marker rather than a
false pass. Neither skip is acceptable in final release evidence. `DOCKER=podman`
uses a temporary local Podman manifest instead of Docker Buildx.

The workflow `.github/workflows/release.yml` exposes the same validation manually,
including `make k8s-compat` across Kubernetes 1.32, 1.34 and 1.37.
With `publish=false` it can run from a branch and has read-only repository permission;
the requested version must still match the planned release version. Publication
additionally requires the selected ref to be the exact existing version tag, a
version absent from both GitHub Releases and GHCR, a clean source tree rechecked
in the privileged job, and a
maintainer's approval of the protected `release` environment.

## Produced artifacts

The release file set contains two raw operator binaries, their SPDX JSON SBOMs, four
engine-binary SPDX JSON SBOMs, the Helm package, complete modified engine source archives and
license files, EgressFox's license, third-party notices, the final image SPDX JSON
SBOM, and `SHA256SUMS`. Raw engine binaries are not separately published; they are
inside the image together with the fixed managed-readiness helper. The OCI image is
both the operator and M7 managed-runtime artifact; no per-Gateway or mutable engine
image is part of the contract. It is published as a multi-platform manifest and is
identified by its registry digest, not by a checksum of the local dry-run OCI tar.

Syft reports Go build dependencies, detectable bundled-engine Go packages and image
OS/file contents. It can omit or misclassify license data, so the explicit engine
notice/source process remains authoritative. Release/build tools are not shipped.

`govulncheck` reports Go vulnerabilities with call-path reachability where known.
Grype reports package/CVE presence for operator/engine SBOMs and the final image; it
does not establish exploitability. High or critical findings fail release validation.
No finding is silently ignored. An exception requires a reviewed repository record
or VEX statement naming artifact/digest, vulnerability, applicability evidence,
owner, expiry, remediation and compensating controls. Expired or unscoped exceptions
are invalid.

## Signing, provenance and publication

The protected publication job first validates the exact tag, commit, planned version,
reviewed changelog section and remote GitHub Release/GHCR tag state. It checks remote
state again immediately before pushing only `ghcr.io/egressfox-io/egressfox:<version>`
and records its immutable digest. GitHub Actions OIDC obtains an ephemeral Sigstore
identity; no private key or registry token is committed. Cosign signs the digest and
attests its SPDX SBOM. GitHub's attestation action creates build provenance for the
image and release files, binding their digests to repository, commit, workflow and
event. The workflow then creates a prerelease for a `dev`, `alpha` or `beta` tag, or
a stable release for a stable tag, using only the reviewed version section from
`CHANGELOG.md`, and uploads the checked files. It never creates
or pushes a Git tag.

The workflow deliberately has no write permission at workflow or validation-job
scope. Only the `publish` job, after the tag checks and protected-environment approval,
receives `contents`, `packages`, `attestations` and OIDC write permissions. It is not
reachable from pull-request execution. Pin updates remain reviewed changes;
Dependabot never auto-merges them.

## User verification

Fetch artifacts from one release, then verify checksums and GitHub provenance:

```sh
sha256sum -c SHA256SUMS
gh attestation verify ./egressfox-operator-0.1.0-dev.1-linux-amd64 \
  --repo egressfox-io/egressfox
gh attestation verify oci://ghcr.io/egressfox-io/egressfox@sha256:REPLACE \
  --repo egressfox-io/egressfox
```

Verify the image's Sigstore identity and SBOM attestation using the exact release
workflow and tag identity:

```sh
cosign verify \
  --certificate-identity 'https://github.com/egressfox-io/egressfox/.github/workflows/release.yml@refs/tags/v0.1.0-dev.1' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  ghcr.io/egressfox-io/egressfox@sha256:REPLACE
cosign verify-attestation --type spdxjson \
  --certificate-identity 'https://github.com/egressfox-io/egressfox/.github/workflows/release.yml@refs/tags/v0.1.0-dev.1' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  ghcr.io/egressfox-io/egressfox@sha256:REPLACE
```

Also inspect `helm show chart`, compare the chart/image version, review the SPDX
SBOM and notices, and install using the verified digest. Signature validity alone
does not establish trust: the repository and workflow identity must match.

## First-release acceptance checklist

- [ ] Release qualification ran from a clean candidate commit with `VERSION` equal to
  the planned release version; the published tag is created afterwards on that commit.
- [ ] `make release-prepare` committed the generated `CHANGELOG.md` before tagging;
  `make release-prepared-check` passes and GitHub Release Notes match that section.
- [ ] The published version is the exact existing tag on that commit, the source tree
  is clean, and no GitHub Release or GHCR version tag exists; a published version is never
  replaced, recreated or reuploaded.
- [ ] `make generate`, `make manifests`, `make check`, `make k8s-compat`,
  `make vuln` and `git diff --check` pass.
- [ ] kind install/upgrade/RBAC/traffic/LKG E2E passes on Kubernetes 1.32, 1.34
  and 1.37.
- [ ] Both Linux operator binaries rebuild identically and report exact metadata.
- [ ] Both input source archives, both licenses, all declared build inputs and the
  reference upstream binary checksums match the manifest; modified-source notices were reviewed.
- [ ] Multi-platform image build succeeds; OCI labels, non-root/read-only defaults,
  embedded licenses and both engine versions are inspected.
- [ ] Helm lint/package/render passes; chart/app/image versions and digest rendering
  agree; generated CRDs match and upgrade/uninstall retention guidance is current.
- [ ] Operator/engine/image SPDX SBOMs exist; `govulncheck` and Grype complete with
  no unexplained release-blocking finding.
- [ ] Sorted SHA-256 manifest verifies every file artifact.
- [ ] In publish mode, image digest signature, SBOM attestation, image/file provenance
  and verification commands pass for the expected repository/workflow identity.
- [ ] SECURITY.md, support matrix, managed-runtime limitations, release notes and
  manual actions are current; only completed P1 behavior is advertised.
- [ ] GitHub private vulnerability reporting and notifications are enabled, and the
  protected `release` environment has required reviewers.

## Manual maintainer actions

Before the first public runtime release, an administrator must enable GitHub private
vulnerability reporting under repository Settings → Security/Advanced Security,
subscribe maintainers/security managers to security-alert notifications, and create
the protected `release` environment with required reviewers and tag restrictions.
These repository settings cannot be truthfully configured or verified from source.
Check GHCR package visibility, repository-linked token access, retention and package
write grants; restrict release-tag creation through a ruleset. The workflow's remote
preflight reads versions for the exact organization container package and treats only
its documented not-found response as an unpublished package; it fails closed if that
read is denied or otherwise fails.
Maintainers must also review current vulnerability results, upstream license/source
availability and the extracted, reviewed changelog notes before approving publication.
