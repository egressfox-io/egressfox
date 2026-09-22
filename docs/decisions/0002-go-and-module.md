# ADR 0002: Go and one small module

Date: 2026-09-18. Status: accepted.

## Context

The control plane needs cancellable network work, deterministic tests, a standalone
binary, and established Kubernetes tooling. Nothing yet justifies multiple language
runtimes, module release cycles, or a public Go library API.

## Decision

Use Go as the primary control-plane/operator language and one module at
`github.com/egressfox-io/egressfox`, matching the existing repository remote.
Keep implementation packages under `internal` when introduced. Entry points remain
thin. Bootstrap adds only a real documentation-check tool, not a fake CLI or
speculative domain interfaces. No Rust workspace is created.

The current toolchain requirement is pinned in [go.mod](../../go.mod); it is an
upgradeable tooling choice, not a forever language-version decision. Bootstrap
verified Go 1.27.1 using the official [release history](https://go.dev/doc/devel/release)
and checksum-verified [downloads](https://go.dev/dl/).

## Alternatives

Rust is plausible for a future rich TUI but would add unnecessary operator
integration/toolchain work now. Multiple modules and a public `pkg` tree would
create compatibility obligations before consumers exist. A generic directory
template would obscure rather than validate component boundaries.

## Consequences

Future packages grow with working behavior and tests. Apply the official
[Go module layout guidance](https://go.dev/doc/modules/layout), explicit dependencies,
consumer-defined interfaces, and normal `gofmt`, `go vet`, and Go testing. Revisit
a separate client/TUI only with a concrete interface and maintenance case.

## Scope clarification (2026-09-23)

[ADR 0015](0015-standalone-runtime-and-engine-packaging.md) accepts the primary
user-facing standalone `egressfox` CLI and long-running control plane in Go over
this shared module. Rust is not the planned primary CLI or a second core. The
historical TUI alternative above remains an undecided, separate client possibility;
it is not part of the standalone product decision.
