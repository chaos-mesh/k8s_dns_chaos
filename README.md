# k8s_dns_chaos[WIP]

## Name

*k8s_dns_chaos* - enables inject DNS chaos in a Kubernetes cluster.

## Description

This plugin implements the [Kubernetes DNS-Based Service Discovery
Specification](https://github.com/kubernetes/dns/blob/master/docs/specification.md).

CoreDNS running the k8s_dns_chaos plugin can be used to do chaos test on DNS.

This plugin can only be used once per Server Block.
