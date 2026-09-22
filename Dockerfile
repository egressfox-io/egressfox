ARG BUILDPLATFORM
ARG TARGETOS=linux
ARG TARGETARCH=amd64
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.23@sha256:96a6b037cd95ee2d72dc63fec59ad8250110fe795111d783d97aa980ec59ec32 AS build
ARG VERSION=0.0.0-dev
ARG REVISION=unknown
ARG CREATED=unknown
ARG SOURCE_DATE_EPOCH=0
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY cmd ./cmd
COPY internal ./internal
COPY tools/releasectl ./tools/releasectl
COPY release ./release
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOMAXPROCS=1 GOMEMLIMIT=1200MiB go build -p=1 -trimpath -buildvcs=false \
    -ldflags="-s -w -X github.com/egressfox-io/egressfox/internal/buildinfo.version=${VERSION} -X github.com/egressfox-io/egressfox/internal/buildinfo.revision=${REVISION} -X github.com/egressfox-io/egressfox/internal/buildinfo.created=${CREATED}" \
    -o /out/egressfox-operator ./cmd/operator
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOMAXPROCS=1 GOMEMLIMIT=1200MiB go build -p=1 -trimpath -buildvcs=false \
    -ldflags="-s -w -buildid=" -o /out/egressfox-healthcheck ./cmd/healthcheck
RUN CGO_ENABLED=0 GOMAXPROCS=1 GOMEMLIMIT=1200MiB go build -p=1 -trimpath -buildvcs=false -ldflags="-s -w" -o /out/releasectl ./tools/releasectl

FROM build AS engine-build
ARG TARGETARCH
RUN /out/releasectl prepare-engine-source --manifest /src/release/manifest.json --engine mihomo --output-dir /engine-src/mihomo && \
    /out/releasectl prepare-engine-source --manifest /src/release/manifest.json --engine sing-box --output-dir /engine-src/sing-box
RUN for dependency in $(/out/releasectl overrides --manifest /src/release/manifest.json --engine mihomo); do cd /engine-src/mihomo && go mod edit -require="$dependency"; done && \
    cd /engine-src/mihomo && go mod tidy && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GOMAXPROCS=1 GOMEMLIMIT=1200MiB go build -p=1 -trimpath -buildvcs=false -tags=with_gvisor \
      -ldflags="-s -w -buildid= -X github.com/metacubex/mihomo/constant.Version=1.19.31" -o /out/mihomo .
RUN for dependency in $(/out/releasectl overrides --manifest /src/release/manifest.json --engine sing-box); do cd /engine-src/sing-box && go mod edit -require="$dependency"; done && \
    cd /engine-src/sing-box && go mod tidy && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GOMAXPROCS=1 GOMEMLIMIT=1200MiB go build -p=1 -trimpath -buildvcs=false \
      -ldflags="-s -w -buildid= -X github.com/sagernet/sing-box/constant.Version=1.14.1" -o /out/egressfox-engine-s ./cmd/sing-box

FROM --platform=$BUILDPLATFORM alpine:3.23.6@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0 AS materials
RUN apk add --no-cache ca-certificates=20260909-r0
COPY --from=build /out/releasectl /usr/local/bin/releasectl
COPY release/manifest.json /release/manifest.json
RUN releasectl fetch-licenses --manifest /release/manifest.json --output-dir /release/licenses && \
    cp /release/licenses/mihomo-LICENSE /release/licenses/GPL-3.0.txt && \
    mkdir -p /release/state && chown 65532:65532 /release/state

FROM scratch
ARG VERSION=0.0.0-dev
ARG REVISION=unknown
ARG CREATED=unknown
LABEL org.opencontainers.image.title="EgressFox operator" \
      org.opencontainers.image.description="Namespace-scoped control plane for desired Mihomo and sing-box configuration" \
      org.opencontainers.image.source="https://github.com/egressfox-io/egressfox" \
      org.opencontainers.image.url="https://github.com/egressfox-io/egressfox" \
      org.opencontainers.image.documentation="https://github.com/egressfox-io/egressfox/tree/${REVISION}/docs" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${CREATED}" \
      org.opencontainers.image.licenses="Apache-2.0 AND GPL-3.0-only AND GPL-3.0-or-later"
COPY --from=build /out/egressfox-operator /usr/local/bin/egressfox-operator
COPY --from=build /out/egressfox-healthcheck /usr/local/bin/egressfox-healthcheck
COPY --from=engine-build /out/mihomo /usr/local/libexec/egressfox/mihomo
COPY --from=engine-build /out/egressfox-engine-s /usr/local/libexec/egressfox/egressfox-engine-s
COPY --from=materials /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=materials /release/state /var/lib/egressfox
COPY --from=materials /release/licenses/ /usr/share/licenses/egressfox/third-party/
COPY LICENSE /usr/share/licenses/egressfox/LICENSE
COPY THIRD_PARTY_NOTICES.md /usr/share/licenses/egressfox/THIRD_PARTY_NOTICES.md
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/egressfox-operator"]
