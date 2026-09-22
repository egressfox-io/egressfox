# Changelog, maintainer governance and CI enforcement

Status: in progress. Prepared: 2026-09-23.

## Objective

Establish a factual initial `CHANGELOG.md`, a permanent maintenance contract,
single-maintainer project guidance, and a read-only pull-request check for notable
change entries and authorized exemptions. Preserve the existing prerelease and
release-publication contract; do not implement product behavior or publish anything.

## Baseline and scope

- Branch: `chore/changelog-governance`.
- Baseline: `baf92d0` on the existing runtime-architecture work branch.
- Repository evidence: no local Git tags; the release guide and `SECURITY.md` state
  that no public release exists. A read-only remote tag lookup could not resolve
  `github.com` in this environment, so no stronger public-release claim is made.
  The completed M1–M7 implementation and release tooling are documented; post-M7
  product work remains planned.
- In scope: changelog, contributor/agent policy, PR template, read-only GitHub
  Actions workflow, standard-library Go validator and tests, maintainer files,
  README navigation, and directly conflicting repository/release guidance.
- Out of scope: application behavior, APIs/CRDs, Helm, Docker, release manifests or
  publication workflows, tags, releases, push and merge.

## Design gates

- Use a maintainer-applied `no-changelog` label together with an explicit PR-body
  request and reason; the request alone is never authorization.
- Automatically exempt only narrowly identified internal documentation and Go test
  file changes. Mixed or ambiguous changes require an `Unreleased` entry or an
  authorized exemption.
- Compare base and head revisions and require a new non-placeholder entry in the
  `Unreleased` section. Do not infer publication from development metadata or a
  successful dry-run.
- Keep the workflow on `pull_request`, with pinned actions and `contents: read`.

## Checkpoints

- [x] Inspect instructions, Git state/history/tags, release contract, completed
  plans, CI, PR template, documentation and current governance files.
- [x] Add the initial factual changelog and align contributor, agent, release and
  repository-settings guidance.
- [x] Add the PR checklist, maintainer documentation, CODEOWNERS and navigation.
- [x] Implement and test the deterministic validator and stable PR workflow.
- [ ] Run the requested repository checks, inspect/stage/commit only task files,
  update this record and leave the branch clean.

## Validation and completion record

Release evidence reviewed: `release/manifest.json` currently plans
`v0.1.0-dev.1`. Completed plans record dry-runs for `v0.1.0-alpha.1`,
`v0.2.0-alpha.1`, and `v0.1.0-dev.1`; the records state that no release was
published, and the release guide says tags are created only for publication. The
current validation and commit record will be added after final review.
