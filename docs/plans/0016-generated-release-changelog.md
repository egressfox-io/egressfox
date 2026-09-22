# Generated changelog from Git history

Status: in progress. Date: 2026-09-23. Branch: `codex/generated-release-changelog`.
Baseline: `c934859`, continuing the unmerged pre-release qualification branch.

## Objective and boundaries

Make emoji-prefixed Conventional Commits the release-note source. An explicit
release-preparation command generates and commits the version section before a tag;
normal CI and protected publication remain read-only with respect to source.
Preserve `v0.1.0-dev.1`, release immutability, and existing historical Git commits.

## Decisions and entry gates

Use the nearest reachable valid release tag as the range baseline; use repository
root history for the first release. Include user-facing `feat`, `fix`, `perf` and
`revert` subjects, classify security fixes separately, preserve each source emoji
once, and exclude internal docs/test/chore/build/CI/refactor commits by default.
Regenerate the target version section deterministically and preserve earlier
version sections. Fail if there are no eligible changes, if versions regress, or if
release preparation runs from a dirty tree, main, or an existing tag.

## Checkpoints

- [x] Implement deterministic commit selection, grouping and changelog rendering.
- [x] Add explicit preparation/commit command and read-only validation/preview tests.
- [x] Replace manual changelog governance, align release documentation and workflow.
- [ ] Generate first release notes on this branch, run full relevant validation and
  non-publishing dry-run, review commits and leave a clean branch.

## Progress and evidence

The current PR check requires agent-written `Unreleased` entries. The release job
already extracts its version section, but the checked-in first-release section was
manually composed. No release tags exist locally or on the read-only remote lookup.
The new Python generator produces Added, Changed, Fixed and Security groups from
eligible history. Its temporary-repository tests cover first and later releases,
deduplication, exclusions, emoji preservation, GitHub note extraction, a single
preparation commit and a stable tagged check. The read-only PR workflow retains the
existing required-check name and tests the generator without modifying source.
Before release preparation, `make fmt`, `make docs`, `make release-validate` and
`make check` passed; 17 Python release tests passed. The first-release preview starts
at repository root and contains 14 Added, 8 Fixed and 13 Security entries. The
Kubernetes 1.32/1.34/1.37 compatibility matrix passed. The explicit preparation
and final clean-commit dry-run remain to be completed.

## Resume and handoff

Implement and test the generator, update repository contracts, then invoke release
preparation as the final source-changing step before qualification. Do not tag,
merge, push or publish.
