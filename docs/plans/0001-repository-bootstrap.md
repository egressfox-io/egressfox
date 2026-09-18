# Repository bootstrap

Status: in progress. Started: 2026-09-18. Branch: `chore/repository-bootstrap`.

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
- [x] Add and validate minimal Go tooling and CI; prepare the tooling checkpoint.
- [ ] Write product truth, architecture, threat model, and accepted ADRs.
- [ ] Write the agent map, workflow, test strategy, roadmap, and next-task plan.
- [ ] Review as a new contributor, validate commands and links, inspect the full diff.
- [ ] Commit completed work; leave a clean branch without merging or pushing.

## Validation and handoff

Tooling validation on 2026-09-18 (Go 1.27.1, macOS arm64):

- `make help fmt check`: passed; includes gofmt, go vet, 14 link-check test cases
  under the race detector, build, offline documentation links, and whitespace checks.
- `make vuln`: passed using pinned govulncheck v1.8.0; no vulnerabilities found.
- Corrected the build target during validation to put executable output in ignored
  `bin/` instead of leaving an accidental root binary; removed that binary.

External engine and Kubernetes tests are not applicable until those adapters exist.
Remaining: finish the authoritative knowledge map, review the complete documentation
and source links, record final validation, and commit/handoff without starting M1.
