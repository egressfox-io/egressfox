ARG SOURCE_DATE_EPOCH=0
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.23@sha256:96a6b037cd95ee2d72dc63fec59ad8250110fe795111d783d97aa980ec59ec32 AS build
ARG GO_MAX_PROCS=2
ARG GO_BUILD_PARALLEL=2
ARG GO_MEMORY_LIMIT=1600MiB
ENV GOMAXPROCS=${GO_MAX_PROCS} GOMEMLIMIT=${GO_MEMORY_LIMIT}
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=egressfox-go-modules,target=/go/pkg/mod,sharing=shared go mod download

FROM build AS release-tool
ARG GO_BUILD_PARALLEL
COPY internal/buildinfo ./internal/buildinfo
COPY internal/artifact ./internal/artifact
COPY internal/releasemanifest ./internal/releasemanifest
COPY tools/releasectl ./tools/releasectl
RUN --mount=type=cache,id=egressfox-go-modules,target=/go/pkg/mod,sharing=shared \
    --mount=type=cache,id=egressfox-go-build,target=/root/.cache/go-build,sharing=shared \
    CGO_ENABLED=0 go build -p=${GO_BUILD_PARALLEL} -trimpath -buildvcs=false -ldflags="-s -w" -o /out/releasectl ./tools/releasectl

FROM build AS application
ARG VERSION=0.0.0-dev
ARG REVISION=unknown
ARG CREATED=unknown
ARG SOURCE_DATE_EPOCH=0
ARG TARGETOS
ARG TARGETARCH
ARG GO_BUILD_PARALLEL
COPY api ./api
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,id=egressfox-go-modules,target=/go/pkg/mod,sharing=shared \
    --mount=type=cache,id=egressfox-go-build,target=/root/.cache/go-build,sharing=shared \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -p=${GO_BUILD_PARALLEL} -trimpath -buildvcs=false \
    -ldflags="-s -w -X github.com/egressfox-io/egressfox/internal/buildinfo.version=${VERSION} -X github.com/egressfox-io/egressfox/internal/buildinfo.revision=${REVISION} -X github.com/egressfox-io/egressfox/internal/buildinfo.created=${CREATED}" \
    -o /out/egressfox-operator ./cmd/operator
RUN --mount=type=cache,id=egressfox-go-modules,target=/go/pkg/mod,sharing=shared \
    --mount=type=cache,id=egressfox-go-build,target=/root/.cache/go-build,sharing=shared \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -p=${GO_BUILD_PARALLEL} -trimpath -buildvcs=false \
    -ldflags="-s -w -buildid=" -o /out/egressfox-healthcheck ./cmd/healthcheck

FROM build AS mihomo-source
COPY --from=release-tool /out/releasectl /usr/local/bin/releasectl
COPY release/manifest.json ./release/manifest.json
RUN releasectl prepare-engine-source --manifest /src/release/manifest.json --engine mihomo --output-dir /engine-src/mihomo
WORKDIR /engine-src/mihomo
RUN --mount=type=cache,id=egressfox-go-modules,target=/go/pkg/mod,sharing=shared \
    for dependency in $(releasectl overrides --manifest /src/release/manifest.json --engine mihomo); do go mod edit -require="$dependency"; done && \
    go mod tidy

FROM build AS sing-box-source
COPY --from=release-tool /out/releasectl /usr/local/bin/releasectl
COPY release/manifest.json ./release/manifest.json
COPY release/overlays/sing-box ./release/overlays/sing-box
RUN releasectl prepare-engine-source --manifest /src/release/manifest.json --engine sing-box --output-dir /engine-src/sing-box
WORKDIR /engine-src/sing-box
RUN --mount=type=cache,id=egressfox-go-modules,target=/go/pkg/mod,sharing=shared \
    for dependency in $(releasectl overrides --manifest /src/release/manifest.json --engine sing-box); do go mod edit -require="$dependency"; done && \
    go mod tidy

FROM mihomo-source AS mihomo-build
ARG TARGETOS
ARG TARGETARCH
ARG GO_BUILD_PARALLEL
RUN --mount=type=cache,id=egressfox-go-modules,target=/go/pkg/mod,sharing=shared \
    --mount=type=cache,id=egressfox-go-build,target=/root/.cache/go-build,sharing=shared \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=readonly -p=${GO_BUILD_PARALLEL} -trimpath -buildvcs=false -tags=with_gvisor \
      -ldflags="-s -w -buildid= -X github.com/metacubex/mihomo/constant.Version=1.19.31" -o /out/mihomo .

FROM sing-box-source AS sing-box-build
ARG TARGETOS
ARG TARGETARCH
ARG GO_BUILD_PARALLEL
RUN --mount=type=cache,id=egressfox-go-modules,target=/go/pkg/mod,sharing=shared \
    --mount=type=cache,id=egressfox-go-build,target=/root/.cache/go-build,sharing=shared \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=readonly -p=${GO_BUILD_PARALLEL} -trimpath -buildvcs=false -tags=with_quic,with_utls \
      -ldflags="-s -w -buildid= -X github.com/sagernet/sing-box/constant.Version=1.14.1" -o /out/egressfox-engine-s ./cmd/sing-box

FROM --platform=$BUILDPLATFORM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS materials
RUN apk add --no-cache ca-certificates=20260909-r0
COPY --from=release-tool /out/releasectl /usr/local/bin/releasectl
COPY release/manifest.json /release/manifest.json
RUN releasectl fetch-licenses --manifest /release/manifest.json --output-dir /release/licenses && \
    cp /release/licenses/mihomo-LICENSE /release/licenses/GPL-3.0.txt && \
    mkdir -p /release/state && chown 65532:65532 /release/state

FROM scratch
ARG VERSION=0.0.0-dev
ARG REVISION=unknown
ARG CREATED=unknown
LABEL org.opencontainers.image.title="EgressFox operator and managed runtime" \
      org.opencontainers.image.description="Namespace-scoped control plane and authenticated managed Mihomo/sing-box runtime" \
      org.opencontainers.image.source="https://github.com/egressfox-io/egressfox" \
      org.opencontainers.image.url="https://github.com/egressfox-io/egressfox" \
      org.opencontainers.image.documentation="https://github.com/egressfox-io/egressfox/tree/${REVISION}/docs" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${CREATED}" \
      org.opencontainers.image.licenses="Apache-2.0 AND GPL-3.0-only AND GPL-3.0-or-later"
COPY --from=application /out/egressfox-operator /usr/local/bin/egressfox-operator
COPY --from=application /out/egressfox-healthcheck /usr/local/bin/egressfox-healthcheck
COPY --from=mihomo-build /out/mihomo /usr/local/libexec/egressfox/mihomo
COPY --from=sing-box-build /out/egressfox-engine-s /usr/local/libexec/egressfox/egressfox-engine-s
COPY --from=materials /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=materials /release/state /var/lib/egressfox
COPY --from=materials /release/licenses/ /usr/share/licenses/egressfox/third-party/
COPY LICENSE /usr/share/licenses/egressfox/LICENSE
COPY THIRD_PARTY_NOTICES.md /usr/share/licenses/egressfox/THIRD_PARTY_NOTICES.md
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/egressfox-operator"]
