# syntax=docker/dockerfile:experimental

FROM golang:1.19 AS build-env
ENV GO111MODULE on
WORKDIR /
RUN git clone --depth 1 --branch v1.7.0 https://github.com/coredns/coredns
COPY . /k8s_dns_chaos
RUN ln -s /k8s_dns_chaos /coredns/plugin/k8s_dns_chaos
RUN echo "k8s_dns_chaos:k8s_dns_chaos" >> plugin.cfg
RUN cd coredns && make

FROM debian:stable-slim AS certs
RUN apt-get update && apt-get -uy upgrade
RUN apt-get -y install ca-certificates && update-ca-certificates

FROM scratch
LABEL org.opencontainers.image.source=https://github.com/chaos-mesh/k8s_dns_chaos
COPY --from=certs /etc/ssl/certs /etc/ssl/certs
COPY --from=build-env /coredns/coredns /coredns
EXPOSE 53 53/udp
ENTRYPOINT ["/coredns"]
