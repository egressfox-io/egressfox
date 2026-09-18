# Repository bootstrap

Status: complete. Started/completed: 2026-09-18. Branch: `chore/repository-bootstrap`.

## Objective and scope

Make the repository sufficient for another agent to implement EgressFox without
the founding conversation. Preserve the existing Apache-2.0 license. This task
adds documentation and development infrastructure only: no endpoint parsers,
probes, scoring, engine rendering, publication, or controllers.

## Discovery and decisions

- Baseline: `c0e1922`, clean `main`, only `LICENSE`; no inherited repository instructions.
- Official Go, Kubernetes, engine, and agent documentation researched before edits.
- Use a Go module for real repository tooling; avoid fake product entry points.
- Defer Kubebuilder generation until API ownership and persistence are reviewed.
- Group designs around data acquisition, observations/selection, desired configuration,
  and Kubernetes integration. Keep priorities, milestones, and decisions distinct.

## Work and checkpoints

- [x] Inspect baseline and create a dedicated task branch.
- [x] Research upstream models and supported tooling.
- [x] Add and validate minimal Go tooling and CI; committed as `d7a94de`.
- [x] Write product truth, architecture, threat model, and accepted ADRs.
- [x] Write the agent map, workflow, test strategy, roadmap, and next-task plan.
- [x] Review as a new contributor, validate commands and links, inspect the full diff.
- [x] Commit completed work; leave a clean branch without merging or pushing.

## Validation and handoff

Tooling validation on 2026-09-18 (Go 1.27.1, macOS arm64):

- `make help fmt check`: passed; includes gofmt, go vet, 14 link-check test cases
  under the race detector, build, offline documentation links, and whitespace checks.
- `make vuln`: passed using pinned govulncheck v1.8.0; no vulnerabilities found.
- Corrected the build target during validation to put executable output in ignored
  `bin/` instead of leaving an accidental root binary; removed that binary.

External engine and Kubernetes tests are not applicable until those adapters exist.
Documentation review:

- All 27 distinct external page URLs returned HTTP 200 during bounded link review.
- All 28 Markdown documents are reachable from README except the separately surfaced
  pull-request template; the root agent contract is 4,338 bytes.
- No empty directories, CRLF text, missing final newlines, or machine-local paths
  were found in the foundation documents. The existing license is unchanged.
- The design explicitly handles observed-versus-published revisions, destination
  freshness, secret-derived identity privacy, and LKG retention versus revocation.

Final validation:

- `make check`: passed after the complete documentation review; formatting, vet,
  race tests, build, local links/anchors, and staged/unstaged whitespace checks passed.
- `make docs`: passed after completion-record updates.
- `git diff --check` and `git diff --cached --check`: passed.
- `ruby -rpsych -e 'Dir[".github/**/*.yml"].each { |path| Psych.parse_file(path) }'`:
  workflow and Dependabot YAML syntax passed. This is a one-off bootstrap check,
  not an additional contributor prerequisite or proof of a remote Actions run.
- Full staged documentation and tooling diffs reviewed; README describes foundation
  status accurately and no product implementation or Kubernetes imitation scaffold exists.

The tooling checkpoint is `d7a94de`; this completion record accompanies the reviewed
documentation checkpoint. Remote CI, native engine validation, and Kubernetes/traffic
tests have not been run or claimed. No push or merge is part of this task.

No implementation milestone was started. The next task is
[M1 endpoint identity and provenance](0002-endpoint-identity.md), beginning with Q1.
