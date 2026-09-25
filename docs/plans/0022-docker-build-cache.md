# Docker build cache and architecture correctness

Status: complete. Date: 2026-09-25. Branch: `codex/docker-build-cache`.
Baseline: `43664c5`, clean working tree on `codex/m85-c4-hysteria2-quic`.

## Objective and boundaries

Reduce repeated container compilation and isolate application and engine invalidation while preserving the [release contract](../operations/releasing.md), pinned inputs and final scratch image. No engine version, dependency override, runtime behavior or publication identity changes are planned.

## Decisions and entry gates

The original Dockerfile ran all builds in one chain with `GOMAXPROCS=1`, `GOMEMLIMIT=1200MiB` and `-p=1`; the engines inherited application copies/builds, so application edits invalidated both. An existing warm local arm64 build was fully cached and completed in 0.76 seconds, so it is not a cold timing baseline. That image was labeled linux/arm64 while its operator was x86-64 because a global `TARGETARCH=amd64` default overrode Buildx's automatic platform argument. This is a release correctness defect and is corrected here.

The release manifest remains the sole engine input authority. Each engine source stage still verifies its pinned archive, applies its overlay and exact overrides, and runs `go mod tidy` once to produce the actual dependency graph. The subsequent compilation uses that graph read-only. BuildKit's local Go module and compiler caches are shared across stages; the protected release job may write a separate registry cache tag only after existing preflight checks. No ordinary CI job receives registry write permission.

## Checkpoints

- [x] Separate host-native release tooling, application and two engine build branches; bound configurable Go parallelism.
- [x] Add persistent Buildx registry cache reuse without changing protected publication and dry-run semantics.
- [x] Verify two architectures, binary identities, cache invalidation and repository checks; commit a clean branch.

## Progress and evidence

The baseline command was `docker buildx build --platform linux/arm64 --progress=plain --load -t egressfox:baseline .`; its warm cached run took 0.76 seconds. Inspection with `file` found an x86-64 operator inside the arm64 image. Cold baseline and improvement figures have not been measured.

The optimized arm64 local build took 461.32 seconds with new Go cache mounts but
pre-existing base/source layers. `go mod tidy` for sing-box and Mihomo took 294.5
and 412.0 seconds respectively, overlapping in BuildKit; the source archives and
licensing inputs remain pinned. A subsequent amd64 build reused the architecture-
independent source stages and took 88.46 seconds. A fully warm arm64 repeat took
0.45 seconds with 37 cached steps. These timings are specific to the local Docker
Desktop builder and do not establish an apples-to-apples cold-build speedup.

The arm64 and amd64 final images each contain four static binaries of the matching
ELF architecture, non-root user 65532:65532, CA certificates, Apache/GPL licenses
and third-party notices. Arm64 engine version commands reported Mihomo 1.19.31
with `with_gvisor` and sing-box 1.14.1 with `with_quic,with_utls`. An explicit
metadata build reported the requested operator version/revision/creation timestamp
and matching OCI labels.

A temporary operator-source comment caused its Go build to run (3.0 seconds) while
both engine compile steps were `CACHED`; the full image took 5.63 seconds. A
temporary sing-box overlay comment reran only that source/tidy/compile branch while
Mihomo remained `CACHED`; the image took 17.13 seconds. Both changes were restored.
The exact dual-platform Buildx OCI export also passed and its index contains
linux/amd64 and linux/arm64 manifests; the warm export took 1.32 seconds.
`make check`, `make docs`, `make release-validate` and `make vuln` passed. The Go
vulnerability scan found no reachable vulnerability and one imported-package
finding without a called vulnerable symbol. Registry cache export/import has not
been exercised against GHCR because only the protected publication job can write
the cache tag.

The centralized `release/manifest.json` remains a shared Docker input, so editing
it invalidates both source-preparation branches even if only one engine record
changes. This preserves manifest validation and is limited to release input updates;
ordinary application edits and a single engine overlay edit are isolated as tested.

## Resume and handoff

No product/engine protocol behavior changed. A future protected release run will
exercise GHCR registry cache export/import and full release qualification on a clean
runner. Cold comparison against the original Dockerfile would require a separate
disposable builder, since pruning this shared builder would destroy unrelated caches.
