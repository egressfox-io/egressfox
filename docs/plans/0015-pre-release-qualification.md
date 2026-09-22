# First public development release qualification

Status: in progress. Date: 2026-09-23. Branch: `codex/pre-release-qualification`.
Baseline: `ed163a1`; clean `main`, eight local commits ahead of `origin/main`, no local tags.

## Objective and boundaries

Audit the actual release path, history, documentation, trust boundaries and existing
runtime tests. Correct release-blocking maintenance defects without adding product
features. Do not push, tag, merge or publish. Preserve Apache-2.0 and engine source
obligations. ADRs 0012 and 0014 and the release guide own the release contract.

## Decisions and entry gates

The checked workflow currently pushes GHCR before checking the GitHub Release and
does not check the registry version tag. This confirms an immutability defect.
`release/manifest.json` defines `v0.1.0-dev.1` as the first version. The changelog
is maintained, but release creation uses independently generated GitHub notes.
These are corrective maintenance issues; no product architecture decision is needed.

## Checkpoints

- [ ] Fail-closed release preflight and workflow ordering with tests.
- [ ] Changelog-based release notes and accurate release documentation.
- [ ] History/security audit, full relevant validation, clean committed handoff.

## Progress and evidence

The release job uses a protected environment, tag check, single-version concurrency,
and pinned Actions. Its current existing-release check occurs after image push and
attestations. GHCR tags are not inherently immutable. The release guard tests only
GitHub Release creation ordering, leaving the image overwrite path unguarded.

## Resume and handoff

Implement the preflight and release-note contract; run non-publishing tests and
release dry-run after changes are committed. Record exact results and limitations.
