# dns_chaos

A standalone CoreDNS plugin used to inject DNS chaos into a Kubernetes cluster. This is the DNS server component for Chaos Mesh's `DNSChaos`.

## Description

This plugin intercepts DNS requests and applies chaos rules (error or random responses) to targeted pods. It works alongside the standard CoreDNS [kubernetes](https://coredns.io/plugins/kubernetes/) plugin.

**Key features:**
- Standalone plugin that doesn't modify the kubernetes plugin
- Sits in front of the kubernetes plugin in the CoreDNS plugin chain
- Supports runtime configuration via gRPC
- Pattern-based domain matching

## Syntax

```txt
dns_chaos {
    grpcport PORT
    kubeconfig KUBECONFIG CONTEXT
}
```

- `grpcport` **PORT** - Port for the gRPC service used for runtime chaos rule updates. Default is `9288`. The gRPC interface is defined in [dns.proto](pb/dns.proto).

- `kubeconfig` **KUBECONFIG** **CONTEXT** - Path to kubeconfig file and context name. If not specified, uses in-cluster config.

## Corefile Configuration

The `dns_chaos` plugin should be placed **before** the `kubernetes` plugin in the Corefile:

```txt
.:53 {
    dns_chaos {
        grpcport 9288
    }
    kubernetes cluster.local in-addr.arpa ip6.arpa {
        pods insecure
        fallthrough in-addr.arpa ip6.arpa
        ttl 30
    }
    forward . /etc/resolv.conf
    cache 30
    loop
    reload
    loadbalance
}
```

## Runtime Configuration via gRPC

Chaos rules are configured at runtime via gRPC. The interface is defined in [dns.proto](pb/dns.proto):

```protobuf
service DNS {
    rpc SetDNSChaos(SetDNSChaosRequest) returns (DNSChaosResponse) {}
    rpc CancelDNSChaos(CancelDNSChaosRequest) returns (DNSChaosResponse) {}
}

message SetDNSChaosRequest {
    string name = 1;
    string action = 2;
    repeated string patterns = 3;
    repeated Pod pods = 4;
}
```

### Actions

- `error` - Return DNS error (SERVFAIL) for matching requests
- `random` - Return random IP addresses for matching requests

### Patterns

Domain patterns support wildcards:
- `google.com` - Match exact domain
- `*.google.com` - Match subdomains
- Empty patterns - Match all domains

## Example

Set chaos rule to return errors for all DNS requests from pod `busybox-0` in namespace `busybox`:

```go
client.SetDNSChaos(ctx, &pb.SetDNSChaosRequest{
    Name:   "my-chaos-rule",
    Action: "error",
    Pods: []*pb.Pod{
        {Namespace: "busybox", Name: "busybox-0"},
    },
})
```

The shell command below will fail:

```sh
kubectl exec busybox-0 -it -n busybox -- ping -c 1 google.com
# Output: ping: bad address 'google.com'
```

To cancel the chaos:

```go
client.CancelDNSChaos(ctx, &pb.CancelDNSChaosRequest{
    Name: "my-chaos-rule",
})
```

## Architecture

```
DNS Request
    │
    ▼
┌─────────────┐
│  dns_chaos  │ ─── Check if source pod has chaos rules
└─────────────┘
    │
    │ (if chaos) ──► Return error/random response
    │
    │ (if no chaos)
    ▼
┌─────────────┐
│ kubernetes  │ ─── Normal DNS resolution
└─────────────┘
    │
    ▼
DNS Response
```
