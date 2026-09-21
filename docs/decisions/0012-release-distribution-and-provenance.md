# ADR 0012: Release distribution and provenance contract

Date: 2026-09-21. Status: accepted.

## Context

M1–M6 produce a working namespace-scoped BYO operator image containing exact Mihomo
and sing-box executables. Q12 prevents public redistribution until their licenses,
corresponding-source duties, notices, private reporting, release ownership, SBOM,
signing and provenance are explicit. The first release must remain a narrow alpha;
release hardening cannot introduce P1 behavior.

The reviewed upstream evidence is the exact
[Mihomo v1.19.31 license](https://github.com/MetaCubeX/mihomo/blob/v1.19.31/LICENSE),
[release](https://github.com/MetaCubeX/mihomo/releases/tag/v1.19.31),
[sing-box v1.14.1 license](https://github.com/SagerNet/sing-box/blob/v1.14.1/LICENSE)
and [release](https://github.com/SagerNet/sing-box/releases/tag/v1.14.1). Mihomo's
license is GPLv3. sing-box states GPL version 3 or later and adds that a derivative
work may not use its name or imply association without prior consent. GPLv3 permits
conveying object code when the license/notices and corresponding source are provided;
it also distinguishes an aggregate of separate independent works from a combined
derivative work. This record is an engineering compliance decision, not legal advice.

## Decision

Release images may include the official, unmodified Mihomo 1.19.31 and sing-box
1.14.1 Linux executables. They are separate processes, not linked with or modified
into EgressFox, so the image is treated as an aggregate: EgressFox remains
Apache-2.0 and each engine retains its upstream terms. Static versus dynamic linking
to EgressFox is therefore inapplicable. Packaging the unchanged binaries in a
container is still conveyance and does not remove their notice or source obligations.

Every release that conveys the engines must include their exact upstream license
files, the full GPLv3 terms, clear factual attribution without endorsement, and the
exact source archives beside the other release assets. Source tags, commit hashes,
download URLs, archive layouts and SHA-256 values live in one reviewed machine-
readable manifest. The build fails closed on any mismatch. Modified or rebuilt
engine executables require a new license/build-source review and conspicuous change
notice before redistribution. Factual engine names identify compatibility only;
no trademark or endorsement right is claimed.

The initial tested and supported profiles are exactly Mihomo 1.19.31 and sing-box
1.14.1 on linux/amd64 and linux/arm64. Exact native validation rejects other
versions. A version may be technically compatible but is unsupported until its
artifact, renderer and controlled tests are reviewed. Kubernetes 1.37.0 is the
initial tested cluster baseline, not a broad minor-version promise.

SemVer prerelease tags such as `v0.1.0-alpha.1` are the release authority. The tag
without `v` feeds the binary metadata, OCI version label/tag, Helm chart version and
appVersion in the packaged chart. The source commit feeds binary and OCI revision;
the commit timestamp supplies `SOURCE_DATE_EPOCH`/OCI creation time. Immutable OCI
digest, rather than a mutable tag, is the deployment identity. The release tooling
rejects version/revision disagreement.

Release construction produces linux/amd64 and linux/arm64 operator archives, a
multi-platform OCI image, a Helm package, SPDX JSON SBOMs, engine source/license
archives and a sorted SHA-256 manifest. Syft, Grype, Cosign and Helm are version-
and-checksum pinned. `govulncheck` covers reachable Go call paths; Grype evaluates
the final image/SBOM including OS packages and detectable bundled-binary packages.
Findings are not auto-ignored. A temporary exception needs a reviewed, scoped,
expiring VEX/record explaining applicability and remediation; presence and
reachability are reported separately.

Ordinary CI and release dry runs are read-only and never receive publication
permissions. Publication runs only from an exact existing SemVer tag through a
manually approved GitHub `release` environment. The trusted job may write packages,
release assets and attestations and request an OIDC token. It pushes the image,
signs its immutable digest keylessly with Cosign, attaches an SBOM attestation and
GitHub build provenance, and attests release files. No long-lived private key is
stored. Users verify the expected repository/workflow identity and digest, not only
that some transparency-log entry exists. The project makes no SLSA-level claim.

GitHub private vulnerability reporting is the selected private channel. Enabling it
and configuring maintainer/security-manager notifications are external repository
settings and remain mandatory maintainer actions before publication. No response
time is promised. Release ownership belongs to maintainers authorized for the
protected `release` environment; a contributor or untrusted pull request cannot
publish.

## Alternatives

Downloading engines at operator startup would avoid EgressFox conveyance but adds a
runtime network dependency, writable executable path and upstream availability
trust at a sensitive boundary. User-provided binaries preserve licensing separation
but make the released operator image incomplete for mandatory validation/probes.
Separate EgressFox-owned engine images still convey the same GPL programs and do not
remove source duties. Long-lived signing keys add secret custody without improving
the GitHub-hosted first-release trust model.

## Consequences

Release assets are larger because exact source and license material accompanies the
binaries. Engine upgrades must update one manifest and repeat license, checksum,
native, traffic and architecture review. Generated SBOM/license metadata supplements
rather than replaces the explicit engine notices. Base repositories, vulnerability
databases, GitHub OIDC/Sigstore and upstream release accounts remain documented
external trust boundaries; pinned inputs and attestations make changes detectable
but cannot prove upstream source or infrastructure was uncompromised.

This resolves Q12's repository decisions. Public release remains blocked until a
maintainer enables GitHub private vulnerability reporting and protects/approves the
`release` environment.
