# Contributing to EgressFox

EgressFox is a pre-release project with a deliberately narrow P0 contract. Changes
are welcome, especially when they improve correctness, security, documentation, or
operational clarity without silently broadening supported behavior.

Before starting substantial work, read the [documentation map](docs/README.md),
[architecture](docs/architecture.md), and [roadmap](docs/roadmap/README.md). Use the
owning design and ADRs for the area you intend to change. Planned capabilities are
not implemented features.

## Development prerequisites

- Git, Make and Python 3.9 or newer (release preflight and notes validation).
- The Go version in [go.mod](go.mod) — currently 1.27.1.
- A C compiler for race-enabled tests.
- Helm for chart linting and rendering.
- Network access for vulnerability scans and first-time tool/module downloads.
- Docker and `kubectl` only for the kind end-to-end suite.

Run `make help` for the maintained command list.

## Workflow

1. Create a focused branch from the current `main`; do not work directly on it.
2. Read the authoritative design and compatibility constraints for your change.
3. For architectural, milestone, or multi-session work, add an
   [execution plan](docs/plans/README.md).
4. Keep behavior, tests, and relevant documentation in the same coherent change.
5. Use the repository's emoji + Conventional Commit format, for example:

   ```text
   🐛 fix(renderer): reject unsupported routing strategy
   ```

Keep the same leading emoji in a pull request title when it represents the commit
used for the merge. Changelog and generated release-note entries retain that source
emoji exactly once while dropping the `type(scope):` prefix. Do not add a duplicate
emoji or rewrite an existing commit message. For example, `✨ feat(gateway): add
managed runtime` becomes `- ✨ Add managed runtime`.

6. Open a pull request explaining the problem, behavior, validation, compatibility
   impact, security implications, and remaining limitations.

Architecture, CRDs and public APIs, persistent state, security boundaries, renderer
semantics, and supported-version changes require explicit compatibility review. Do
not add speculative interfaces, placeholder packages, or P1 behavior as a shortcut.
The detailed Git contract is in the [development workflow](docs/development/workflow.md).

## Changelog

`CHANGELOG.md` is generated from Git history during explicit release preparation.
Coding agents and contributors do not maintain `Unreleased` entries by hand. Write
accurate emoji-prefixed Conventional Commit subjects for user-facing changes:
`feat`, `fix`, `perf` and `revert` become public notes; security fixes are grouped
separately. Documentation, tests, routine CI/build/dependency changes and internal
refactors are excluded. A rare eligible commit may opt out with `Changelog: skip`
in its body or `[skip changelog]` in its subject; explain the reason in that commit.
Do not use the marker to hide a user-visible fix or compatibility change.

On a dedicated release-preparation branch, a maintainer runs
`VERSION=vX.Y.Z... make release-prepare` from a clean worktree. This generates the
new section from commits after the nearest previous release tag (or repository root
for the first release) and commits only `CHANGELOG.md`. Review the generated text
and correct misleading commit subjects through a new follow-up commit before
preparation; do not rewrite shared history or hand-edit release entries. If a new
eligible commit follows preparation, rerun the command before tagging. The tagged
source must contain the generated section. `make release-prepared-check` compares it
with Git history without editing files. Ordinary CI and public release publication
never create release-preparation commits. GitHub Release Notes use the same section;
see the [release guide](docs/operations/releasing.md).

## Validation

For documentation-only changes:

```sh
make docs
git diff --check
```

For normal code or repository changes:

```sh
make check
make vuln
```

Add the relevant integration layer when the change affects it:

```sh
make test-envtest       # Kubernetes API/controller behavior
make e2e-kind           # real cluster, Helm, RBAC and controlled traffic
make release-validate   # release manifest/version/notice consistency
```

Release-affecting work also runs the non-publishing dry run documented in the
[release guide](docs/operations/releasing.md). Never describe an unavailable or
skipped check as passing.

## Generated files

API and RBAC changes must regenerate and commit their owned outputs:

```sh
make generate
make manifests
make generate-check
```

Review generated CRD differences as API changes. Do not hand-edit generated
deep-copy code, generated CRDs, or generated RBAC in isolation.

## Security and test data

Never commit real subscription URLs, proxy URIs, credentials, Kubernetes Secret
contents, generated engine configuration, private endpoint revisions, or local
state databases. Tests use clearly synthetic credentials and reserved example
domains. Follow [SECURITY.md](SECURITY.md) for sensitive findings rather than
opening a public issue.

Critical engine, Kubernetes, controller-runtime, base-image, and release-tool
updates require compatibility tests plus license, SBOM, and vulnerability review.
Automated dependency proposals are never merged without review.

## Community expectations

Be specific, respectful, and patient. The project currently has no guaranteed
support or response SLA. A formal Code of Conduct will be added when maintainers
establish a real moderation contact and enforcement process; none is invented in
this repository today.
