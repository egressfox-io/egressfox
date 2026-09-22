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

Publication checks both the target GitHub Release and the GHCR version tag before
the first remote write, then repeats the check immediately before image push. An
existing image without a GitHub Release is a partial publication and also blocks a
rerun. Release assets are uploaded without clobbering. Recovery requires a new
version; neither the workflow nor routine recovery deletes or replaces artifacts.

## Qualification and publication requirements

Pre-publication qualification and publication have different requirements. A dry run
prepares and inspects candidate artifacts; it does not need a Git tag, because the
tag is created only for publication and is not a prerequisite for building evidence.

| Step | Required |
| --- | --- |
| `make release-dry-run` | Clean worktree, and `VERSION` equal to the planned release version (`release.developmentVersion` in `release/manifest.json`, which the chart mirrors) |
| Publication | Exact existing Git tag for that version on the selected commit, clean source, approval of the protected `release` environment, and an immutable version that has never been published |

Dry-run qualification on a clean untagged commit reports the commit-derived identity
`0.1.0-dev.1+g<commit>` in the artifacts. Publication is strictly tag-bound: the
protected workflow runs only from the exact tag, rechecks the tag and a clean tree in
the privileged job, and refuses a version that already exists.

## Maintainer release sequence

1. Choose an allowed version and update `release/manifest.json` plus the chart's
   `version`/`appVersion` to it in a reviewed commit; do not tag from a pull request.
2. Run `VERSION=vX.Y.Z-alpha.N make release-dry-run` from that clean commit. The
   requested version must match the planned release version, and the dry run fails
   closed on any non-ignored source change. No Git tag is needed for this step, and
   there is no dirty override: commit or stash first.
3. Review the release acceptance checklist, compatibility evidence, SBOMs,
   vulnerability reports, notices and generated GitHub release notes.
4. Create the matching signed or reviewed Git tag on that exact reviewed commit by
   the project’s normal maintainer process, then manually dispatch the protected
   release workflow on the tag. Its non-publishing validation is separate from the
   privileged publication job, which refuses an existing release.
5. For a published release, verify the tag, immutable image digest, provenance,
   signatures, checksums and Helm metadata as described in the
   [release guide](releasing.md).

`CHANGELOG.md` is the maintained development record: add notable changes under
`Unreleased` during ordinary development. Before tagging, finalize the applicable
entries under `## [vX.Y.Z...]` in the reviewed commit and leave a fresh `Unreleased`
section. The version section is a prepared release candidate, not a claim of
publication; GitHub Releases records the actual publication date. The workflow
extracts exactly that section into release notes and rejects a missing, duplicate or
empty section before image publication. It never generates a second narrative from
pull requests. Preserve the leading semantic emoji from each Conventional Commit
subject exactly once in reader-facing entries.

## Updating the development line

Changing the planned development version changes `release/manifest.json` and the
chart's source `version`/`appVersion` together. `make release-validate` rejects
drift. A published release derives its version from the tag and does not require a
commit that manually edits chart metadata for each release.

`make release-validate` also runs `releasectl release-guard`, which fails when the
publication workflow stops refusing an existing release, reintroduces asset
clobbering, or loses its exact-tag, clean-tree, publish-input or protected-environment
checks. The guard reads workflow structure and shell tokens, so harmless YAML
formatting changes never fail CI while a lost guarantee does. Because `make check`
and the release validation job both depend on it, an integrity regression fails
ordinary CI as well as release qualification. Ordinary dry runs and local `dist/`
directories remain repeatable because immutability applies to publication, not to
local output.
