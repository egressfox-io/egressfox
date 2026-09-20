# syntax=docker/dockerfile:1.7
FROM golang:1.27.1-alpine3.23@sha256:96a6b037cd95ee2d72dc63fec59ad8250110fe795111d783d97aa980ec59ec32 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOMAXPROCS=1 GOMEMLIMIT=1200MiB go build -p=1 -trimpath -ldflags="-s -w" -o /out/egressfox-operator ./cmd/operator

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS engines
ARG TARGETARCH=amd64
ARG MIHOMO_VERSION=1.19.31
ARG SING_BOX_VERSION=1.14.1
RUN apk add --no-cache ca-certificates curl tar
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) mihomo_sha=d5e74bbddbdfff49a1aef7775bf5911da59f0d7196ed509a0ac914b3653dd5f1; sing_sha=b907365b154e4a7e3e40be15c2cd83433c0fa65c7dc736bdb1b5face2afe4501 ;; \
      arm64) mihomo_sha=9e0f11afbf38426b8bd88fdc594678f8161c57eccb4e1b77acb12b493904f1d4; sing_sha=d94fc9704372ca2fa2854e54c20b406e4b8779b5ccdd0c557da90ea9344e9631 ;; \
      *) echo "unsupported architecture" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/mihomo.gz "https://github.com/MetaCubeX/mihomo/releases/download/v${MIHOMO_VERSION}/mihomo-linux-${TARGETARCH}-v${MIHOMO_VERSION}.gz"; \
    echo "$mihomo_sha  /tmp/mihomo.gz" | sha256sum -c -; \
    gunzip -c /tmp/mihomo.gz > /usr/local/bin/mihomo; chmod 0555 /usr/local/bin/mihomo; \
    curl -fsSL -o /tmp/sing-box.tar.gz "https://github.com/SagerNet/sing-box/releases/download/v${SING_BOX_VERSION}/sing-box-${SING_BOX_VERSION}-linux-${TARGETARCH}-musl.tar.gz"; \
    echo "$sing_sha  /tmp/sing-box.tar.gz" | sha256sum -c -; \
    mkdir /tmp/sing-box; tar -xzf /tmp/sing-box.tar.gz -C /tmp/sing-box --strip-components=1; \
    install -m 0555 /tmp/sing-box/sing-box /usr/local/bin/sing-box

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659
RUN apk add --no-cache ca-certificates && mkdir -p /var/lib/egressfox && chown 65532:65532 /var/lib/egressfox
COPY --from=build /out/egressfox-operator /usr/local/bin/egressfox-operator
COPY --from=engines /usr/local/bin/mihomo /usr/local/bin/mihomo
COPY --from=engines /usr/local/bin/sing-box /usr/local/bin/sing-box
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/egressfox-operator"]
