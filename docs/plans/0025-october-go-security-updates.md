# October Go security updates

Status: complete.
Date: 2026-10-10. Branch: fix/october-go-vulnerabilities. Baseline: d5ffaaa.

## Objective and boundaries

Resolve every finding from `make vuln` with the smallest compatible security
updates. Preserve engine versions, renderer profiles, protocol support and
Kubernetes compatibility. No publishing, tagging, pushing or release qualification
claims. The [release guide](../operations/releasing.md) owns build provenance.

## Decisions and entry gates

The initial pinned govulncheck scan reported 11 reachable advisories in Go 1.27.1
and HTTP/2 advisories in golang.org/x/net before v0.60.0. The upstream fixes are
Go 1.27.2 and x/net v0.60.0. The verbose scan also identified the
imported, unreachable [GO-2026-6094](https://pkg.go.dev/vuln/GO-2026-6094) in
cel-go v0.29.2; upgrade it to v0.30.0 to remove that finding too. Update the minimum Go version, digest-pinned Docker
builder, release engine build toolchains and relevant dependency requirements.
Keep capability profiles and feature-tag build revisions unchanged; native
receipts already include the toolchain and exact dependency overrides, so older
binaries are invalidated automatically.

## Checkpoints

- [x] Synchronize patched toolchain, dependency graph and build provenance.
- [x] Repeat vulnerability scan and applicable repository validation.
- [x] Review, commit and record any release/native qualification limitations.

## Progress and evidence

The initial `make vuln` failed with 11 reachable advisories, including
[GO-2026-6617](https://pkg.go.dev/vuln/GO-2026-6617). A network-restricted attempt
could not reach the module proxy; the subsequent online scan completed and
reported the vulnerabilities. The official Docker registry's Go 1.27.2 Alpine
3.23 image index digest was fetched and verified against its response body.

Prepared engine source now carries the manifest Go minimum and preferred
toolchain. This closes a host-build gap: changing into an upstream module with
its older Go requirement could otherwise select an older installed compiler,
even though the repository and manifest had been patched. Regression tests cover
preservation of upstream dependencies and rejection of a newer upstream minimum.

Executed validation:

- `make vuln`: PASS, no reachable, imported-package or module findings remain.
- `make check`: PASS after dependency updates; generated CRDs, vet, full race
  tests, operator/tools builds, documentation, Helm and release validation passed.
- After adding the prepared-source toolchain guard,
  `go test -race -count=1 ./tools/releasectl ./internal/releasemanifest` and
  `go vet ./tools/releasectl ./internal/releasemanifest`: PASS.
- `make build-tools docs release-validate`: PASS after the guard.
- Both real checksum-pinned upstream source archives were prepared under
  `dist/security-source-mihomo-go1272` and `dist/security-source-sing-box-go1272`;
  their module declarations and `go env GOVERSION` select Go 1.27.2.
- Final vulnerability scan after all code changes: PASS.
- Formatting and whitespace checks: PASS.

## Resume and handoff

The vulnerability maintenance task is complete. Dependency maintenance is excluded
from generated release notes by the existing changelog policy. Prior M9 evidence
applies to the previous build inputs; this task does not requalify the full
Kubernetes/native release matrix, rebuild engine binaries, or perform Docker/image
scans. Native receipts with older toolchain/override inputs are rejected and need
rebuilding for subsequent native qualification. Nothing is pushed, merged, tagged
or published.

## Dry-run follow-up

The maintainer's full `v0.1.0-dev.2` dry run at `6041a50` completed without skip
markers, including deterministic amd64/arm64 builds, image construction, source
vulnerability scans, SBOM scans and checksums. Grype's remaining module-level
GO-2026-5932 findings are scoped in the
[applicability review](../security/go-2026-5932.md), including artifact hashes and
fresh dependency-graph checks for both exact Linux architectures.

Update Syft to 1.54.1 and Grype to 0.120.1 using official release checksum lists for
all four tool platforms. Disable only their repeated application-update notices;
keep database refresh and all security findings visible. Buffer successful Python
fixture output in release validation so synthetic remote preflight messages do
not resemble actual publication checks. Full dry run for the updated scanner pins
is pending; preserve the previous artifacts as evidence.

Follow-up validation: `make release-validate docs` passed (26 Python tests,
manifest/publication guard and offline links), `sh -n hack/release-dry-run.sh`
and whitespace checks passed. Both updated host tools installed through the
repository's checksum-verifying installer; `config --load` confirmed application
update checks are disabled for each. No full dry run, engine rebuild or Kubernetes
suite was executed for this follow-up. Routine tooling/dependency maintenance is
excluded from the generated changelog by policy.
