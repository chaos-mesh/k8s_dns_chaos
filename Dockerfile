# syntax=docker/dockerfile:1

FROM golang:1.27.2 AS build-env
ARG TARGETARCH
WORKDIR /app
COPY . .
RUN --mount=type=cache,id=chaos-dns-go-mod,target=/go/pkg/mod \
    --mount=type=cache,id=chaos-dns-go-build-${TARGETARCH},target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o coredns ./cmd/coredns

FROM debian:stable-slim AS certs
RUN apt-get update && apt-get -uy upgrade
RUN apt-get -y install ca-certificates && update-ca-certificates

FROM scratch
LABEL org.opencontainers.image.source=https://github.com/chaos-mesh/k8s_dns_chaos
COPY --from=certs /etc/ssl/certs /etc/ssl/certs
COPY --from=build-env /app/coredns /coredns
EXPOSE 53 53/udp
ENTRYPOINT ["/coredns"]
