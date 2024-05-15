FROM --platform=$BUILDPLATFORM golang:1.22-bullseye AS builder

ARG GOARCH=''

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

ARG TARGETOS
ARG TARGETARCH

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GO111MODULE=on go build -ldflags="-s -w" -a -o fedhcp main.go

FROM debian:stable as installer

RUN apt-get update \
  && apt-get -y install --no-install-recommends libcap2-bin \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/*

FROM gcr.io/distroless/static-debian12

WORKDIR /

COPY --from=builder /workspace/fedhcp .
COPY --from=installer /sbin/setcap /sbin/setcap
COPY --from=installer /lib/x86_64-linux-gnu/libcap.so.2 /lib/x86_64-linux-gnu/libcap.so.2
COPY --from=installer /lib/x86_64-linux-gnu/libc.so.6 /lib/x86_64-linux-gnu/libc.so.6
COPY --from=installer /lib64/ld-linux-x86-64.so.2 /lib64/ld-linux-x86-64.so.2
COPY --from=installer /bin/sh /bin/sh

RUN /sbin/setcap 'cap_net_bind_service,cap_net_raw=+ep' /fedhcp

USER 65532:65532

ENTRYPOINT ["/fedhcp"]
