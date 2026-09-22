# ADR 0014: development versioning and Kubernetes compatibility

Status: Accepted. Date: 2026-09-22.

## Context

The first-release hardening contract established checksum-pinned inputs and a
manually gated publication workflow, but it accepted only alpha tags and repeated a
single Kubernetes 1.37 assumption in several tools. That made an untagged build
ambiguous and made compatibility claims difficult to reproduce or extend.

The M7 operator uses controller-runtime v0.25 and Kubernetes Go libraries v0.37.
Those application dependencies are not a reason to silently redefine the server
versions the operator can exercise. The product needs a narrow, evidence-based
Kubernetes contract that distinguishes EgressFox qualification from Kubernetes
upstream maintenance.

## Decision

`release/manifest.json` is the machine-readable authority for the planned
development version, release platforms, and pinned Kubernetes qualification
profiles. It records the controller-tools envtest version plus its official SHA-512
for every supported host platform and the digest-pinned kind node image. Tooling
fails closed for unknown minor versions, missing artifacts and digest mismatches.

The initial planned development release is `v0.1.0-dev.1`; this ADR creates no tag.
Official EgressFox tags are exactly one of:

- `vX.Y.Z-dev.N` for development snapshots deliberately published for testing;
- `vX.Y.Z-alpha.N` and `vX.Y.Z-beta.N` for prereleases; or
- `vX.Y.Z` for a stable release.

`N` is a positive integer. Other SemVer prerelease labels and build metadata are
not release tags. The tag with its leading `v` is the Git/GitHub identity. The same
tag without `v` is the binary version, OCI version/tag, Helm package `version`,
Helm `appVersion`, and default chart image tag. A release workflow requires an exact
tag on the selected commit; prerelease channels become GitHub prereleases and a
stable tag becomes a GitHub release. Generated GitHub notes are release notes, with
maintainer review required before protected publication.

A published version is immutable: it identifies exactly one commit, image digest and
release file set. Publication refuses a target GitHub Release that already exists and
uploads assets without clobbering, so an existing version can be neither replaced nor
recreated. Every change advances the version — `v0.1.0-dev.1` → `v0.1.0-dev.2` →
`v0.1.0-dev.3` → `v0.1.0-alpha.1`. Recovery from a defective publication is a new
version; deleting, editing or regenerating a published one is not a supported
operation. Repository tooling validates that the protected workflow keeps refusing
an existing release, never passes `--clobber`, and keeps its exact-tag,
clean-tree and protected-environment gates.

An untagged Git build is `0.1.0-dev.1+g<12-lowercase-commit-characters>`, a
non-Git local build is `0.1.0-dev.1+local`, and an untagged build whose tree has
non-ignored changes appends `.dirty` to that metadata. SemVer build metadata does not
affect precedence and is never a publishable release tag. Ignored build/release
output does not mark a tree dirty. `make build` injects that bounded version together
with full revision and commit timestamp into the operator's existing `--version`
output. A tagged commit reports exactly its release version with no metadata and is
never built from a dirty tree.

Qualification and publication have different requirements, and the tag is only part
of publication. A release dry run is pre-publication qualification: it requires a
clean working tree and a requested version equal to the planned release version in
the manifest and chart, and deliberately works from an untagged candidate commit with
the commit-derived identity above. Publication is strictly tag-bound: it requires the
exact existing Git tag on the selected commit, a clean source tree rechecked in the
privileged job, approval of the protected `release` environment, and a version that
has never been published. Creating the tag is therefore part of publishing, not of
building qualification evidence.

The Kubernetes compatibility contract has a tested minimum of 1.32 and release
qualification profiles 1.32, 1.34 and 1.37. `make test-envtest`, `make helm-check`
and `make e2e-kind` select a profile with `K8S_VERSION=1.<minor>`; the default is the
minimum. `make k8s-compat` exercises all three profiles. The regular CI covers the
minimum profile; scheduled/manual trusted CI and release validation run the complete
matrix. The chart declares `>=1.32.0-0`, but that install constraint is not a claim
that unqualified newer minors are supported.

The compatibility guide records executed qualification evidence. A Kubernetes
minor remains supported only while it is listed there with current evidence. This is
independent of [Kubernetes upstream version support](https://kubernetes.io/releases/version-skew-policy/#supported-versions);
users must independently account for the lifecycle of their cluster release.

## Consequences

Release inputs no longer drift between Make, envtest, kind, Helm and release scripts.
Maintainers add a profile by updating one reviewed manifest record with official
artifact provenance and running the full matrix before documenting support. The
oldest profile makes ordinary CI more conservative while keeping expensive
multi-cluster E2E out of every pull request.

Version identity now fails closed at every boundary: a dirty tree cannot produce a
release-qualification identity, a version that is not the planned release version
cannot be qualified or published, and a published version cannot be published again.
Correcting a defective snapshot therefore costs a version increment, while
pre-publication qualification stays possible on the reviewed candidate commit before
its tag exists.

The existing [ADR 0012](0012-release-distribution-and-provenance.md) remains the
authority for redistribution, SBOMs, vulnerability handling, signing and
provenance. This ADR supersedes its alpha-only tag wording.

No Kubernetes API, CRD storage version, persistence schema, managed runtime
semantics or P1 feature is changed by this decision. Compatibility remains an
operational qualification claim, not a guarantee for every CNI, admission policy,
distribution or future Kubernetes minor.
