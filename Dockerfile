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
    CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH GO111MODULE=on go build -ldflags="-s -w" -a -o fedhcp main.go

FROM gcr.io/distroless/static-debian11
WORKDIR /
COPY --from=builder /workspace/fedhcp .
USER 65532:65532

ENTRYPOINT ["/fedhcp"]
