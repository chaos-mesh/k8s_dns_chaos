# syntax=docker/dockerfile:experimental

FROM golang:1.19 AS build-env
ENV GO111MODULE on
WORKDIR /
RUN git clone https://github.com/coredns/coredns && git checkout 7d5f5b87a4fb310d442f7ef0d52e3fead0e10d39
COPY . /k8s_dns_chaos
# RUN ln -s /k8s_dns_chaos /coredns/plugin/k8s_dns_chaos
RUN echo "k8s_dns_chaos:github.com/chaos-mesh/k8s_dns_chaos" >> /coredns/plugin.cfg
RUN cd coredns && \
    go mod edit -require github.com/chaos-mesh/k8s_dns_chaos@v0.0.0-00000000000000-000000000000 && \
    go mod edit -replace github.com/chaos-mesh/k8s_dns_chaos=/k8s_dns_chaos && \ 
    go mod edit -replace google.golang.org/grpc=google.golang.org/grpc@v1.29.1 && \
    go get github.com/chaos-mesh/k8s_dns_chaos && \
    go generate && \
    go mod tidy
RUN cd coredns && make

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
