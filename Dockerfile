FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.9.0 AS xx
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder

COPY --from=xx / /
RUN apt-get update && apt-get install -y --no-install-recommends clang lld

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY plugins/ plugins/
COPY internal/ internal/
COPY api/ api/

ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

# See:
# - https://github.com/tonistiigi/xx#xx-apk-xx-apt-xx-apt-get---installing-packages-for-target-architecture
# - https://github.com/tonistiigi/xx#go--cgo
# - https://github.com/tonistiigi/xx#xx-verify---verifying-compilation-results
RUN xx-apt-get install -y --no-install-recommends gcc libc6-dev
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    CGO_ENABLED=1 xx-go build -a -o fedhcp main.go \
    && xx-verify fedhcp

FROM debian:stable AS installer

RUN apt-get update \
  && apt-get -y install --no-install-recommends libcap2-bin \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/*

FROM gcr.io/distroless/base-debian12 AS distroless-base

FROM distroless-base AS distroless-amd64
ENV LIB_DIR_PREFIX=x86_64
ENV LINKER=ld-linux-x86-64.so.2

FROM distroless-base AS distroless-arm64
ENV LIB_DIR_PREFIX=aarch64
ENV LINKER=ld-linux-aarch64.so.1

FROM distroless-$TARGETARCH AS output-image

WORKDIR /

COPY --from=builder /workspace/fedhcp .
COPY --from=installer /sbin/setcap /sbin/setcap
COPY --from=installer /lib/${LIB_DIR_PREFIX}-linux-gnu/libcap.so.2 /lib/${LIB_DIR_PREFIX}-linux-gnu/libcap.so.2
COPY --from=installer /lib/${LIB_DIR_PREFIX}-linux-gnu/libgcc_s.so.1 /lib/${LIB_DIR_PREFIX}-linux-gnu/libgcc_s.so.1
COPY --from=installer /lib/${LIB_DIR_PREFIX}-linux-gnu/libc.so.6 /lib/${LIB_DIR_PREFIX}-linux-gnu/libc.so.6
COPY --from=installer /lib/${LIB_DIR_PREFIX}-linux-gnu/${LINKER} /lib/${LIB_DIR_PREFIX}-linux-gnu/${LINKER}
COPY --from=installer /bin/sh /bin/sh

RUN /sbin/setcap 'cap_net_bind_service,cap_net_raw=+ep' /fedhcp

USER 65532:65532

ENTRYPOINT ["/fedhcp"]
