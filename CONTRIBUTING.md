# Contributing

EgressFox has completed the M1–M6 P0 implementation and is preparing an alpha release. Start with
the [documentation map](docs/README.md) and the
[next milestone](docs/roadmap/README.md). Planned capabilities are not implemented
features. The existing [Apache-2.0 license](LICENSE) is unchanged.

For a small fix, make the change and its relevant checks directly on a task branch.
For a substantial change, read the owning design, resolve its blocking questions,
and write a short repository [execution plan](docs/plans/README.md). Architecture,
CRDs/public APIs, security boundaries, persistent state, and renderer semantics
need deliberate design/compatibility consideration alongside code.

The [development workflow](docs/development/workflow.md) is authoritative for
branching, agent-owned commits, emoji Conventional Commits, setup, and definition
of done. Agents must create dedicated branches, validate and commit their work,
and leave a clean reviewable branch. Do not automatically merge or rewrite history.

Run `make check` and `make vuln` before handing off applicable changes, plus the
feature-specific tests in the [testing strategy](docs/development/testing.md).
Release-affecting changes also run `make release-validate` and the non-publishing
dry run in the [release guide](docs/operations/releasing.md). Critical engine,
Kubernetes, controller-runtime, base-image and release-tool pins require compatibility,
license/SBOM and vulnerability review; Dependabot suggestions are never auto-merged.
Explain the resulting behavior, validation evidence, compatibility impact, and
remaining limitations in the pull request. Never submit real credentials,
subscriptions, or generated operational configs as fixtures.

For sensitive findings, follow [security reporting](SECURITY.md).
