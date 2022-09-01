FROM golang:1.19-bullseye AS builder
WORKDIR /tmp/fedhcp
ADD . ./
RUN make target/fedhcp

FROM gcr.io/distroless/static-debian11
COPY --from=builder /tmp/fedhcp/target/fedhcp /usr/bin/fedhcp
COPY --from=builder /tmp/fedhcp/config.yml /etc/fedhcp/config.yml

CMD ["/usr/bin/fedhcp", "-c", "/etc/fedhcp/config.yml"]