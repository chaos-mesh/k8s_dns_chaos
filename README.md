# k8s_dns_chaos

`dns_chaos` is a standalone CoreDNS plugin that injects DNS faults for selected Kubernetes Pods. It provides the DNS server used by Chaos Mesh `DNSChaos`.

The plugin identifies target Pods by the request's source IP and applies rules received through gRPC. Requests without a matching fault continue to the standard CoreDNS `kubernetes` plugin or another plugin, such as `forward`. The server builds from [cmd/coredns](cmd/coredns) and registers `dns_chaos` immediately before `cache`, ahead of `kubernetes`, independent of Corefile stanza order.

## Build the server image

Use Docker with BuildKit to build CoreDNS with this plugin:

```sh
make image
```

The image tag is `ghcr.io/chaos-mesh/chaos-coredns:latest`. The [Dockerfile](Dockerfile) builds the local `cmd/coredns` package. The image includes `/coredns` and CA certificates.

To build an executable for your local operating system and architecture:

```sh
make coredns
```

Use `make help` to list build targets. The build toolchain and dependency versions are defined in [go.mod](go.mod) and [Dockerfile](Dockerfile).

## Configure and run DNS

The server needs access to the Kubernetes API. Both `dns_chaos` and `kubernetes` use in-cluster credentials by default. Grant access to Services, Pods, Namespaces, and `discovery.k8s.io` EndpointSlices. The [E2E RBAC manifest](e2e/manifests/chaos-coredns-rbac.yaml) provides a deployment example.

For an in-cluster server, save this configuration as `Corefile`:

```txt
.:53 {
	errors
	ready
	dns_chaos {
		grpcport 9288
	}
	kubernetes cluster.local in-addr.arpa ip6.arpa {
		pods insecure
		fallthrough in-addr.arpa ip6.arpa
		ttl 30
	}
	forward . /etc/resolv.conf
	reload
}
```

Mount the Corefile into the container and pass `-conf /etc/coredns/Corefile`. See the [E2E Deployment](e2e/manifests/chaos-coredns-deployment.yaml) and [Service](e2e/manifests/chaos-coredns-service.yaml) for DNS and gRPC ports.

For a server outside the cluster, add `kubeconfig /etc/kubernetes/config CONTEXT` inside both plugin blocks. Mount the kubeconfig and its referenced credential files. This example runs that configuration in Docker:

```sh
docker run --rm --name chaos-coredns \
	-p 1053:53/udp -p 1053:53/tcp -p 127.0.0.1:9288:9288 \
	-v "$PWD/Corefile:/etc/coredns/Corefile:ro" \
	-v "$PWD/kubeconfig:/etc/kubernetes/config:ro" \
	ghcr.io/chaos-mesh/chaos-coredns:latest \
	-conf /etc/coredns/Corefile
```

The API address and credentials must work from inside the container. GCP kubeconfigs must use an external exec credential provider, such as `gke-gcloud-auth-plugin`. The old `auth-provider: gcp` configuration no longer works with the current client-go dependency. The scratch image does not include external credential executables.

Target Pods must send their DNS queries to this server, and the server must see each Pod's source IP. A gRPC fault rule alone does not change a Pod's DNS configuration. The Chaos Mesh controller handles that redirection for `DNSChaos` experiments. The [direct gRPC test Pod](e2e/manifests/test-pod.yaml) uses `dnsPolicy: None` and points `dnsConfig.nameservers` to the chaos DNS Service.

The CoreDNS `cache` plugin can remain enabled. Fault responses bypass it, so cached normal answers cannot hide rule changes and injected answers cannot reach other Pods through the shared cache. Requests without a matching fault still use the normal DNS cache. Client-side DNS caches can retain answers until their TTL expires.

### Upgrade from the forked plugin

Update the Corefile and RBAC together with the DNS server image:

1. Replace `k8s_dns_chaos` with separate `dns_chaos` and `kubernetes` directives as shown above. Move `grpcport` into `dns_chaos`. Keep supported Kubernetes options, such as `pods`, `fallthrough`, and `ttl`, in `kubernetes`.
2. If you use `kubeconfig`, configure it in both plugin blocks. Remove static `chaos ACTION SCOPE` directives and configure faults through `DNSChaos` or gRPC patterns.
3. Preserve the listener address, port, Service target ports, and other Corefile directives. If the deployment listens on port `5353`, keep that port.
4. Add the following rule to the ClusterRole bound to the DNS deployment's ServiceAccount. Retain its existing permissions.

```yaml
rules:
- apiGroups: ["discovery.k8s.io"]
  resources: ["endpointslices"]
  verbs: ["get", "list", "watch"]
```

The standard CoreDNS `kubernetes` plugin watches EndpointSlices. Existing Chaos Mesh DNS roles may lack these permissions. For an installation in `chaos-mesh` using the `chaos-dns-server` ServiceAccount, check the list permission with:

```sh
kubectl auth can-i list endpointslices.discovery.k8s.io \
	--as=system:serviceaccount:chaos-mesh:chaos-dns-server
```

The command must return `yes`. Use your deployment's namespace and ServiceAccount name if they differ.

## Corefile reference

The parser in [chaos/setup.go](chaos/setup.go) accepts these options:

```txt
dns_chaos {
	grpcport PORT
	kubeconfig KUBECONFIG CONTEXT
}
```

| Option | Behavior |
| --- | --- |
| `grpcport PORT` | Sets the gRPC TCP port from `1` through `65535`. The default is `9288`. The listener binds on all interfaces and uses plaintext gRPC. |
| `kubeconfig KUBECONFIG CONTEXT` | Uses the specified kubeconfig file and context. Both arguments are required. Without this option, the plugin uses in-cluster credentials. |

The separate [CoreDNS kubernetes plugin](https://coredns.io/plugins/kubernetes/) controls normal cluster DNS records and their visibility. `dns_chaos` has no static fault or scope directive.

Rules and the gRPC service survive CoreDNS reloads. A `grpcport` change keeps the old port open until the replacement DNS server starts. If replacement DNS startup fails, the old service remains available and the candidate port closes. All `dns_chaos` directives in one process must use the same configuration. Changes to Kubernetes client configuration, including changes inside the kubeconfig file, require a process restart.

## Change faults through gRPC

The [protobuf schema](pb/dns_chaos.proto) preserves the legacy `pb.DNS` service. The method paths are `/pb.DNS/SetDNSChaos` and `/pb.DNS/CancelDNSChaos`. Generated Go messages and clients are in [pb/](pb).

`SetDNSChaos` accepts a rule name, an action, target Pods, and optional domain patterns. Each target Pod must have an assigned IP address. The actions are:

- `error` returns DNS `SERVFAIL` for matching queries.
- `static` returns the configured IP for an exact domain name. Each `ipDomainMaps` entry contains `domain` and `ip`. Names are case-insensitive and allow a trailing dot. A matching A or AAAA query receives the mapped address with TTL 10. A query for the other address family succeeds with no answers. Unmapped names and other query types continue to the next plugin.
- `random` returns a random IPv4 address for an A query or a random IPv6 address for an AAAA query. Other query types continue to the next CoreDNS plugin. Synthetic answers have a TTL of 10 seconds.

An empty `patterns` list affects all queried names. Patterns are case-insensitive, and trailing dots are ignored. Supported patterns include exact names, a final `*` for zero or more characters, `?` for one character, and character classes such as `[12]`, `[1-3]`, and `[!9]`. A `*` anywhere except the final position is rejected, including `*.example.com`. The legacy `scope` and `selector` fields remain in the schema for wire compatibility. Non-empty values are rejected with gRPC `InvalidArgument`.

For example, send this request through `pb.NewDNSClient(conn).SetDNSChaos`:

```go
&pb.SetDNSChaosRequest{
	Name:     "example-fault",
	Action:   "error",
	Pods:     []*pb.Pod{{Namespace: "busybox", Name: "busybox-0"}},
	Patterns: []string{"example.com", "chaos-test.local*"},
}
```

For a static rule, set `Action: "static"` and include mappings:

```go
IpDomainMaps: []*pb.IpDomainMap{
	{Domain: "example.com", Ip: "192.0.2.10"},
	{Domain: "ipv6.example.com", Ip: "2001:db8::10"},
},
```

Static rules require at least one mapping. Each name maps to one IP address. Invalid domain names, invalid IPs, and duplicate names are rejected before changing the rule. Optional `patterns` further restrict the mapped names that take effect.

Each Pod can belong to one active rule. Setting an existing rule name adds or updates the supplied Pod targets and preserves the other targets under that name. A Pod targeted by another rule, or an IP already assigned to another target, causes `AlreadyExists`.

Call `CancelDNSChaos` with `&pb.CancelDNSChaosRequest{Name: "example-fault"}` to remove every target under that rule. Cancelling an unknown name succeeds.

Rules exist in memory and do not survive a process restart. The [gRPC tests](chaos/grpc_server_test.go) exercise both legacy method paths, target accumulation, conflicts, and cancellation with a fake Kubernetes client. The [reload tests](chaos/reload_test.go) exercise successful and failed reloads and gRPC port changes.

## Develop and test

Use the Go version declared in [go.mod](go.mod). Check the active shared cache locations before installing tools or building:

```sh
go env GOCACHE GOMODCACHE GOPATH
```

Reuse those caches across branches and worktrees. The Dockerfile also uses shared BuildKit cache mounts for module downloads and Go builds.

Run the checks used by [CI](.github/workflows/e2e-tests.yaml):

```sh
make check
```

This target runs unit and race tests, compiles the E2E tests, runs `go vet`, and checks module consistency. The unit tests cover fault responses, patterns, Pod IP refresh, the gRPC contract, and CoreDNS reload behavior. E2E compilation does not execute cluster tests.

### Regenerate protobuf code

Install `protoc` through your system package manager, then run:

```sh
make protoc
```

The Makefile resolves `protoc-gen-go` and `protoc-gen-go-grpc` with `go tool -n`. Their versions are locked by the root module's `tool` declarations and dependency graph. Commit schema changes together with the generated files in `pb/`, and preserve the `pb.DNS` service and existing field numbers for client compatibility.

### Run the cluster E2E suite

The suite requires Bash, Go, Docker with BuildKit, minikube, kubectl, Helm, and access to the image registries and build dependencies.

```sh
./e2e/e2e.sh
```

The script builds the `e2e-test` image and creates a unique Docker-driver minikube profile with containerd and a private kubeconfig. It uses a dynamically assigned local gRPC port and deletes its own profile on exit.

The suite installs Chaos Mesh `2.8.5`, runs direct gRPC tests, and creates real `DNSChaos` `error` and `random` experiments. The controller tests verify two target Pods, an unaffected control Pod, domain matching, and resolver and DNS recovery after CR deletion. Each target uses a distinct probe domain. The E2E Corefile disables DNS caching so every query reaches the plugin.

Set `E2E_ARTIFACT_DIR` to retain logs at a chosen path. The default is a temporary directory outside the repository. On failure, the script collects controller, daemon, and DNS logs before deleting its cluster.

## License

[Apache License 2.0](LICENSE).
