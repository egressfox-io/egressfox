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
`0.1.0-dev.1+local`. A working tree with non-ignored changes appends `.dirty`, so a
dirty build can never claim the identity of a clean build from the same commit:

| Source tree | Reported identity |
| --- | --- |
| Clean untagged commit `abcdef123456` | `0.1.0-dev.1+gabcdef123456` |
| Same commit with uncommitted changes | `0.1.0-dev.1+gabcdef123456.dirty` |
| Commit tagged `v0.1.0-dev.1` | `0.1.0-dev.1` |

Ignored build and release output (`dist/`, `.cache/`, `bin/`) is not source and never
marks the tree dirty. These local identities are not publishable release artifacts.
The build also reports the full revision, creation timestamp, Go version and exact
engine profiles. Inspect it with:

```sh
make build
./bin/egressfox-operator --version
```

## Published versions are immutable

A published version identifies exactly one immutable set of release artifacts.
`v0.1.0-dev.1` must correspond to one commit, one image digest and one release file
set forever. Changes always advance the version:

    v0.1.0-dev.1 → v0.1.0-dev.2 → v0.1.0-dev.3 → v0.1.0-alpha.1

Publication refuses to continue when the target GitHub Release exists, and release
assets are uploaded without clobbering, so an existing version can be neither
replaced nor recreated. Recovery from a bad publication is a new version, never an
edit or deletion of a published one.

## Maintainer release sequence

1. Choose an allowed version and create the matching signed or reviewed Git tag by
   the project’s normal maintainer process; do not create it from a pull request.
2. Run `VERSION=vX.Y.Z-alpha.N make release-dry-run` from the exact tagged commit
   with a clean working tree. The explicit version must be the release tag on `HEAD`,
   and the dry run fails closed on any non-ignored source change. There is no dirty
   override; commit or stash first.
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

`make release-validate` also runs `releasectl release-guard`, which fails when the
publication workflow stops refusing an existing release, reintroduces asset
clobbering, or loses its tag, clean-tree and protected-environment checks. Because
`make check` and the release validation job both depend on it, an integrity
regression fails ordinary CI as well as release qualification. Ordinary dry runs and
local `dist/` directories remain repeatable because immutability applies to
publication, not to local output.
