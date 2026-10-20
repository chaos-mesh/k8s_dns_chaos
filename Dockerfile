# syntax=docker/dockerfile:1

FROM golang:1.27.2 AS build-env
ARG TARGETARCH
WORKDIR /
RUN git clone --depth 1 --branch v1.14.7 https://github.com/coredns/coredns
COPY . /k8s_dns_chaos
RUN sed -i '/kubernetes/a\k8s_dns_chaos:github.com/chaos-mesh/k8s_dns_chaos' /coredns/plugin.cfg
RUN --mount=type=cache,id=chaos-dns-go-mod,target=/go/pkg/mod \
    --mount=type=cache,id=chaos-dns-go-build-${TARGETARCH},target=/root/.cache/go-build \
    cd coredns && \
    go mod edit -require github.com/chaos-mesh/k8s_dns_chaos@v0.0.0-00000000000000-000000000000 && \
    go mod edit -replace github.com/chaos-mesh/k8s_dns_chaos=/k8s_dns_chaos && \
    go get github.com/chaos-mesh/k8s_dns_chaos && \
    go generate && \
    go mod tidy && \
    make

FROM debian:stable-slim AS certs
RUN apt-get update && apt-get -uy upgrade
RUN apt-get -y install ca-certificates && update-ca-certificates

FROM scratch
LABEL org.opencontainers.image.source=https://github.com/chaos-mesh/k8s_dns_chaos
COPY --from=certs /etc/ssl/certs /etc/ssl/certs
COPY --from=build-env /coredns/coredns /coredns
EXPOSE 53 53/udp
ENV GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn
ENTRYPOINT ["/coredns"]
