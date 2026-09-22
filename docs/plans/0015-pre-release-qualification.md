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

- [x] Fail-closed release preflight and workflow ordering with tests.
- [x] Changelog-based release notes and accurate release documentation.
- [ ] History/security audit, full relevant validation, clean committed handoff.

## Progress and evidence

The original release job used a protected environment, tag check, per-version concurrency,
and pinned Actions. Its current existing-release check occurs after image push and
attestations. GHCR tags are not inherently immutable. The release guard tests only
GitHub Release creation ordering, leaving the image overwrite path unguarded.

Checkpoint `e0f45fa` adds a read-only GitHub Release and package-version preflight
before registry publication, repeated immediately before image push; one global
non-canceling concurrency group; fail-closed partial-publication behavior; exact
changelog release notes; executable mocked preflight/notes tests; and workflow
ordering guards. The first planned version remains `v0.1.0-dev.1`.

`make fmt`, `make generate`, `make manifests`, `make helm-check`, `make check`,
`make test-envtest`, `make vuln`, and default Kubernetes 1.32 `make e2e-kind`
passed. `make vuln` found zero reachable vulnerabilities and one imported-package
finding without a called vulnerable symbol. A full non-publishing
`VERSION=v0.1.0-dev.1 make release-dry-run` passed on clean `e0f45fa`; its release
directory contains both operator architectures, the Helm package, binary/image
SPDX SBOMs, both modified engine source archives, licenses/notices and a verified
SHA256SUMS. Grype reported no high or critical finding; an Unknown-severity Go
advisory for x/crypto remains in the image/engine reports and govulncheck found no
reachable engine call path. The [Go advisory](https://pkg.go.dev/vuln/GO-2026-5932)
affects the unmaintained OpenPGP subpackages; the prepared engine source has no
direct OpenPGP import. This is module-level scanner noise rather than evidence of a
reachable shipped OpenPGP path. No image, package, tag, release or asset was published.

The local history has 86 commits and no tags; `origin/main` is an ancestor eight
commits behind the initial local `main`, and a read-only remote query found no
remote tags. A heuristic scan of reachable objects found no high-confidence private
keys, GitHub tokens or cloud keys; proxy URI matches use example or local-test hosts.
One historical 43 MB archive contains an older source snapshot and compiled tools;
its inspected text entries had no high-confidence secret markers. The archive remains
in history because this task forbids history rewriting. The scan is heuristic, not a
guarantee that no secret exists.

## Resume and handoff

Complete the full Kubernetes profile matrix and run the full dry-run on the final
clean commit. Inspect the final diff, record results, and leave the task branch
committed and clean. External repository protection and GHCR settings still require
maintainer verification before the first public release.
