# k8s_dns_chaos

Used to inject DNS chaos into a Kubernetes cluster. This is the DNS server for the Chaos Mesh `DNSChaos`.

## Description

This plugin implements the [Kubernetes DNS-Based Service Discovery Specification](https://github.com/kubernetes/dns/blob/master/docs/specification.md).

CoreDNS running with the k8s_dns_chaos plugin can be used to do chaos tests on DNS.

This plugin can only be used once per Server Block.

> **Note:** It works with CoreDNS 7d5f5b87a4fb310d442f7ef0d52e3fead0e10d39.

## Syntax

```sh
k8s_dns_chaos [ZONES...]
```

The _k8s_dns_chaos_ supports all options in plugin _[kubernetes](https://coredns.io/plugins/kubernetes/)_, besides, it also supports other configuration items for chaos.

```txt
kubernetes [ZONES...] {
    endpoint URL
    tls CERT KEY CACERT
    kubeconfig KUBECONFIG CONTEXT
    namespaces NAMESPACE...
    labels EXPRESSION
    pods POD-MODE
    endpoint_pod_names
    ttl TTL
    noendpoints
    transfer to ADDRESS...
    fallthrough [ZONES...]
    ignore empty_service

    chaos ACTION SCOPE [PODS...]
    grpcport PORT
}
```

Only `[ZONES...]`, `chaos` and `grpcport` are different from the _[kubernetes](https://coredns.io/plugins/kubernetes/)_ plugin:

- `[ZONES...]` defines which zones of the host will be treated as internal hosts in the Kubernetes cluster.

- `chaos` **ACTION** **SCOPE** **[PODS...]** sets the behavior and scope of chaos.

  Valid values for **Action**:
  - `random`: return random IP for DNS request.
  - `error`: return error for DNS request.

  Valid values for **SCOPE**:
  - `inner`: chaos only works on the inner host of the Kubernetes cluster.
  - `outer`: chaos only works on the outer host of the Kubernetes cluster.
  - `all`: chaos works on all the hosts.

  **[PODS...]** defines which Pods will take effect, the format is `Namespace`.`PodName`.

- `grpcport` **PORT** sets the port of GRPC service, which is used for the hot update of the chaos rules. The default value is `9288`. The interface of the GRPC service is defined in [dns.proto](pb/dns.proto).

## Examples

All DNS requests in Pod `busybox.busybox-0` will get error:

```txt
k8s_dns_chaos cluster.local in-addr.arpa ip6.arpa {
    pods insecure
    fallthrough in-addr.arpa ip6.arpa
    ttl 30
    chaos error all busybox.busybox-0
}
```

The shell command below will execute failed:

```sh
# Output: ping: bad address 'google.com'
kubectl exec busybox-0 -it -n busybox -- ping -c 1 google.com
```
