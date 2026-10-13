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

- `grpcport` **PORT** - Port for the gRPC service used for runtime chaos rule updates. Default is `9288`. The gRPC interface is defined in [dns_chaos.proto](pb/dns_chaos.proto).

- `kubeconfig` **KUBECONFIG** **CONTEXT** - Path to kubeconfig file and context name. If not specified, uses in-cluster config.

## Corefile Configuration

The custom `cmd/coredns` binary registers `dns_chaos` immediately before the CoreDNS `kubernetes` plugin, independent of the Corefile stanza order. Keep both directives in the same server block:

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

Rules and the gRPC service survive CoreDNS reloads. A `grpcport` change keeps the old port open until the replacement DNS server starts. If replacement DNS startup fails, the old service remains available and the candidate port closes. All `dns_chaos` directives in one CoreDNS process must use the same configuration. Changes to Kubernetes client configuration, including changes inside the kubeconfig file, require a process restart.

## Runtime Configuration via gRPC

Chaos rules are configured at runtime via gRPC. The service keeps the `pb.DNS` name used by existing Chaos Mesh controllers. The interface is defined in [dns_chaos.proto](pb/dns_chaos.proto):

```protobuf
service DNS {
    rpc SetDNSChaos(SetDNSChaosRequest) returns (DNSChaosResponse) {}
    rpc CancelDNSChaos(CancelDNSChaosRequest) returns (DNSChaosResponse) {}
}

message SetDNSChaosRequest {
    string name = 1;
    repeated Pod pods = 2;
    string action = 3;
    repeated string patterns = 6;
}
```

### Actions

- `error` - Return DNS error (SERVFAIL) for matching requests
- `random` - Return random IPv4 or IPv6 addresses for matching `A` or `AAAA` requests. Other record types continue to the next CoreDNS plugin.

### Patterns

Domain patterns support wildcards:

- `google.com` - Match exact domain
- `chaos-mesh.*` - `*` matches zero or more characters and must be the last character
- `github.?om` - `?` matches exactly one character
- `db[12].example.com`, `db[1-3].example.com`, and `db[!9].example.com` - Match a character set, range, or excluded set
- Omitted `patterns` (or an empty list) - Match all domains

The proto's legacy `scope` and `selector` fields are retained for wire compatibility but are not implemented by this standalone plugin. Requests that set either field are rejected with gRPC `InvalidArgument`.

Each pod can belong to one active rule. Setting an existing rule name adds or updates the supplied pod targets. Canceling the rule removes every pod added under that name. A target pod must have an assigned IP address before a rule can be created.

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
