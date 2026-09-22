# Development workflow

This is the authoritative Git, contribution, tooling, and definition-of-done
contract for repository work. [CONTRIBUTING.md](../../CONTRIBUTING.md) is the human
entry point; [AGENTS.md](../../AGENTS.md) adds constraints for automated coding tools.

## Setup and commands

Use macOS or Linux, Git, Make, and the Go version required by
[go.mod](../../go.mod). Install Go from the official
[download page](https://go.dev/dl/) and verify the published checksum. Release
hardening was validated with Go 1.27.1. A C compiler is needed for Go's race detector; GitHub's
Ubuntu runner includes one. No Kubernetes cluster, engine binary, Docker, Node,
Python, or Rust installation is required for the current checks.

From the repository root:

```sh
make help
make fmt
make check
make vuln
VERSION=v0.1.0-dev.1 make release-dry-run
```

| Command | Behavior today |
| --- | --- |
| `make fmt` | Format tracked and non-ignored new Go files with gofmt |
| `make lint` | Non-mutating formatting check and `go vet ./...` |
| `make test` | `go test -race -count=1 ./...`; covers repository tooling and all current Go domains |
| `make build` | Builds the operator with bounded Git-derived version/revision metadata and the repository tooling |
| `make docs` | Offline repository-local Markdown file/heading-link checks |
| `make check` | Lint, tests, build, docs, and unstaged/staged whitespace checks |
| `make vuln` | Pinned govulncheck from Makefile; requires module/vulnerability database access |
| `make release-validate` | Validate engine/tool metadata, notices, Helm image-version flow and publication immutability without network access |
| `make release-dry-run` | Qualify a clean candidate commit for the planned `VERSION` (no Git tag needed) by constructing binaries, source/license files, SPDX SBOMs, image, Helm package, scans and checksums without publishing |
| `make k8s-compat` | Run envtest, Helm and kind E2E across all release-qualification Kubernetes profiles |

The module's direct and transitive dependencies are pinned by `go.mod`/`go.sum`. The
versioned `go run ...@version` security tool does not add a product dependency.
Caches use normal Go locations; constrained environments can set `GOCACHE` and
`GOMODCACHE` to writable directories without editing the repository. Do not commit
downloaded binaries, caches, credentials, or real configurations. Release outputs
belong under ignored `dist/`, and checksum-verified tools under ignored `.cache/`;
do not commit either.

`make check` works offline with the toolchain installed and, once dependencies are
introduced, available in cache. `make vuln` is deliberately separate because its
database access is online. Network failure is not a successful vulnerability scan.

## Permanent Git contract

Contributors own their completed commits. Do not leave a finished task as a large,
unreviewed working-tree diff for someone else to sort out.

1. Inspect branch, status, recent history, repository instructions, and the relevant
   design. Preserve all pre-existing user changes; never stage unrelated files.
2. Create a dedicated descriptive branch for each coherent task. Substantial work
   must never happen directly on `main`. Examples: `feat/endpoint-model`,
   `feat/probe-engine`, `fix/renderer-validation`, `docs/security-model`,
   `refactor/selection-engine`, `chore/repository-maintenance`. When resuming the same
   task, verify its branch and plan rather than creating needless replacement branches.
3. For work spanning components, architectural changes, a milestone, or multiple
   sessions, create/update an [execution plan](../plans/README.md) before implementation.
4. Complete a coherent checkpoint with its behavior tests and relevant design/docs.
5. Before **every commit**, inspect the diff, run relevant validation, stage only
   intentional paths, inspect `git diff --cached` and `git diff --cached --check`,
   then commit with the convention below.
6. Use multiple meaningful, atomic commits for substantial work; avoid both a giant
   final dump and micro-commits or WIP commits. Keep tests with behavior or a clearly
   related atomic change. Update the plan at checkpoint boundaries.
7. Run full applicable validation, inspect the complete branch diff, and leave
   completed work committed on a clean, reviewable branch. Do not merge into `main`
   or push without explicit instruction. Handoff includes branch, commits, results,
   unresolved limitations, and the next unit of work if relevant.

Never destructively reset, discard/overwrite user changes, force-push, or rewrite
user/shared history without explicit permission. Do not auto-stash another person's
changes or change their Git identity. If a clean handoff is impossible because
pre-existing changes must remain, preserve them and report that exception precisely;
do not clean them up to satisfy a checklist.

### Commit convention

Use Conventional Commits with semantic emojis:

```text
<emoji> <type>(<optional-scope>): <description>
```

The type remains authoritative; the emoji is a visual signal. Describe the actual
engineering change, never `update`, `changes`, `work`, `WIP`, or `fix stuff`. Use an
imperative, specific description and a body when rationale
or compatibility impact is not obvious.

Keep the same leading emoji in the pull request title when it represents the
Conventional Commit subject used for the merge. Changelog and release-note summaries
retain that source emoji exactly once while dropping the Conventional Commit prefix;
do not add a second emoji. For example, `✨ feat(gateway): add managed runtime`
becomes `- ✨ Add managed runtime`.

| Change | Preferred signal and example |
| --- | --- |
| Feature | `✨ feat(core): add normalized endpoint model` |
| Bug fix | `🐛 fix(renderer): reject unsupported routing strategy` |
| Refactor | `♻️ refactor(selection): separate scoring from filtering` |
| Tests | `✅ test(renderer): add mihomo golden tests` |
| Documentation | `📝 docs(architecture): define control-plane boundaries` |
| Security | `🔒 fix(security): redact endpoint credentials` |
| Performance | `⚡ perf(probe): bound scheduler allocations` |
| Routine tooling | `🔧 chore(ci): add repository validation workflow` |
| Repository/build architecture | `🏗️ chore(repo): establish Go tooling foundation` |
| Dependencies | `⬆️ chore(deps): update controller-runtime` |
| CI/release | `🚀 ci(release): publish multi-architecture images` |
| Removal | `🗑️ chore: remove obsolete tooling` |
| Critical hotfix | `🚑 fix(publish): preserve last-known-good on write failure` |

Use the normal Conventional Commit breaking-change marker/body when applicable;
an emoji does not communicate compatibility by itself.

## Design and implementation discipline

Substantial changes start with the relevant design and its
[open questions](../decisions/open-questions.md). Architecture, public API/CRD,
security, storage, and renderer semantic changes require deliberate compatibility
review. Update the authoritative design alongside code; add/supersede an ADR for
durable changed decisions. An execution plan is a progress record, not a parallel
source of product truth. Tiny local fixes do not need an ADR or a plan.

Keep Go packages cohesive, entry points thin, errors contextual and sanitized,
I/O cancellable, and dependencies explicit. Define interfaces at actual consumers.
Use deterministic ordering and controlled clock/randomness where decisions require
replay. Do not add `utils`, global mutable state, speculative frameworks, public
packages without consumers, or interfaces merely to anticipate PostgreSQL.

Create an adapter package only with its first real behavior and tests; see the
[placement map](../architecture.md). Do not add placeholder methods returning nil
or fake controllers. New dependencies need a purpose, maintenance/security/license
review, and a pinned version. Do not import engine protocol implementations into
the core as a shortcut.

## CI and tool maintenance

[Repository checks](../../.github/workflows/check.yml) run on pull requests, main
pushes, scheduled/manual full-compatibility dispatch and with read-only repository
permissions and no secrets.
Actions are pinned to full commit IDs, following
[GitHub's secure-use guidance](https://docs.github.com/en/actions/reference/security/secure-use).
Do not switch to `pull_request_target` to execute untrusted code with privileges.
The workflow is configured here; a remote run/branch-protection setting is not
claimed by local validation.

[Dependabot](../../.github/dependabot.yml) covers Actions, Go modules, and the
Dockerfile. Review updates, preserve compatible Kubernetes version sets, and run
the same checks. The Go toolchain requirement in go.mod and scanner pin in Makefile
need deliberate review; a bot configuration alone is not an update guarantee.
Add richer linting, release scanning/signing, and generated-file checks only when
real code or releases justify them. `go vet` is the current lightweight lint pass.

The documentation checker deliberately supports repository-relative inline links,
ATX headings with simple text, and fenced code blocks. Use those forms for internal
navigation; no reference-style links, HTML anchors, or complex linked heading text.
It skips remote HTTP(S) availability and illustrative fenced snippets; review those
manually. It is not a full Markdown renderer or a secret scanner. Avoid machine-local
absolute paths and bare nonportable file links in repository documentation.

## Definition of done

- Scope and invariants respected; implemented/planned/future wording is accurate.
- Review [the changelog policy](../../CONTRIBUTING.md#changelog): add a factual
  `CHANGELOG.md` entry under `Unreleased` for notable changes, or record why no
  entry is required. Mention the decision in the final handoff; milestone completion
  does not create a release section.
- Behavior changes have meaningful tests from the [testing strategy](testing.md).
- Relevant designs, API compatibility notes, threat model, ADRs, and plan are current.
- Documented applicable commands run successfully; blocked/unavailable checks are
  reported honestly. Generated artifacts, when introduced, reproduce cleanly.
- Links and terminology reviewed; no credentials, unintended local paths, dead
  configuration, empty speculative directories, or accidental binaries in the diff.
- Complete diff and staged changes reviewed; commits use the required convention;
  task-owned changes are committed and the branch is clean (preserving/reporting
  any pre-existing unrelated changes). No automatic merge into main.
