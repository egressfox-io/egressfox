# Development and release versioning

[ADR 0014](../decisions/0014-development-versioning-and-kubernetes-compatibility.md)
is the authoritative decision. The planned first development version is
`v0.1.0-dev.1`; it is a repository value, not a tag created by this guide.

## Version forms

| Purpose | Allowed Git tag | GitHub release type |
| --- | --- | --- |
| Deliberate development snapshot | `vX.Y.Z-dev.N` | Prerelease |
| Alpha | `vX.Y.Z-alpha.N` | Prerelease |
| Beta | `vX.Y.Z-beta.N` | Prerelease |
| Stable | `vX.Y.Z` | Release |

`N` is a positive integer. Tags with another prerelease name, a leading-zero
number, or SemVer build metadata are rejected. The leading `v` is retained for Git
tags. Without it, the value is used consistently for the operator `--version`, OCI
tag/label, Helm `version`, Helm `appVersion`, and default image tag.

An untagged build reports `0.1.0-dev.1+g<commit>`; a build outside Git reports
`0.1.0-dev.1+local`. These are local identities only and must not be published as
release artifacts. The build also reports the full revision, creation timestamp,
Go version and exact engine profiles. Inspect it with:

```sh
make build
./bin/egressfox-operator --version
```

## Maintainer release sequence

1. Choose an allowed version and create the matching signed or reviewed Git tag by
   the project’s normal maintainer process; do not create it from a pull request.
2. Run `VERSION=vX.Y.Z-alpha.N make release-dry-run` from the exact tagged commit.
   The explicit version requirement prevents an untagged checkout from being
   mistaken for a release.
3. Review the release acceptance checklist, compatibility evidence, SBOMs,
   vulnerability reports, notices and generated GitHub release notes.
4. Manually dispatch the protected release workflow on that exact tag. Its
   non-publishing validation is separate from the privileged publication job.
5. For a published release, verify the tag, immutable image digest, provenance,
   signatures, checksums and Helm metadata as described in the
   [release guide](releasing.md).

GitHub-generated notes are the initial changelog convention. A maintainer must add
important compatibility/migration/security context and verify that prerelease versus
stable classification matches the tag before approval. Do not hand-maintain a second
unsynchronized changelog.

## Updating the development line

Changing the planned development version changes `release/manifest.json` and the
chart's source `version`/`appVersion` together. `make release-validate` rejects
drift. A published release derives its version from the tag and does not require a
commit that manually edits chart metadata for each release.
