# Third-party notices

EgressFox itself is licensed under Apache-2.0. Release artifacts also contain or
describe third-party software. This inventory is informational and is not legal
advice. The release SBOM is the machine-generated dependency inventory; this file
records the separately bundled executables whose redistribution needs an explicit
human-readable notice.

## Bundled engine executables

| Program | Exact version | Source and corresponding source | License |
| --- | --- | --- | --- |
| Mihomo derivative | 1.19.31, build revision 1, tag `v1.19.31`, commit `ab405bad5beeeac8b003bb01f60f134f6df54471` | [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo/tree/ab405bad5beeeac8b003bb01f60f134f6df54471); complete release source `mihomo-1.19.31-egressfox.1-source.tar.gz`; pinned input `mihomo-1.19.31-source.tar.gz` | GPL-3.0-only; `mihomo-LICENSE` and `GPL-3.0.txt` in the image and release license directory |
| EgressFox Engine S, derived from sing-box | compatible with 1.14.1, build revision 1, tag `v1.14.1`, commit `1ac1a339cb1223e9c70eae14c44411c75033c02d` | [SagerNet/sing-box](https://github.com/SagerNet/sing-box/tree/1ac1a339cb1223e9c70eae14c44411c75033c02d); complete release source `sing-box-1.14.1-egressfox.1-source.tar.gz`; pinned input `sing-box-1.14.1-source.tar.gz` | GPL-3.0-or-later plus the upstream no-derivative-name/association condition; `sing-box-LICENSE` and `GPL-3.0.txt` in the image and release license directory |

The release process downloads exact source archives and license files only from URLs
and SHA-256 values in [`release/manifest.json`](release/manifest.json), then builds
the separate executables using its recorded Go version, minimum dependency requirements and
feature tags. Checksum, overlay, build or scan failure is fatal. Complete modified
source includes `EGRESSFOX-CHANGES.md` and the resulting module files. Inclusion and
factual compatibility statements do not imply sponsorship or endorsement by
MetaCubeX, SagerNet or their contributors. In particular, EgressFox Engine S is not
named sing-box and is not associated with the upstream application.

The complete build source archives and license files are published beside every public
EgressFox release that conveys these executables. A distributor mirroring an image
or release must preserve the notices and ensure corresponding source remains
available as required by the applicable license.

## Other dependency classes

- The operator binary's Go modules are recorded in its SPDX JSON SBOM. Module
  versions are pinned by `go.mod`/`go.sum`; the SBOM generator may not identify every
  license and its output still requires review.
- The container image's executable and data-file contents are recorded in the image
  SPDX JSON SBOM. The final `scratch` image contains no OS package manager or OS
  packages; its CA bundle is copied from a pinned build-stage package.
- Syft, Grype, Cosign, Helm, kind, envtest and controller-gen are release/build/test
  tools, not files shipped in the operator image. Their versions and download
  checksums are pinned in the repository.

See the release guide for obtaining and verifying SBOMs, checksums, source archives,
signatures and provenance.
