# k8s_dns_chaos

`k8s_dns_chaos` is a CoreDNS plugin that injects DNS faults for selected Kubernetes Pods. It provides the DNS server used by Chaos Mesh `DNSChaos`.

The plugin contains a fork of the CoreDNS Kubernetes DNS backend. It resolves cluster Services, endpoints, and Pod records, then applies faults according to the requesting Pod's source IP. A gRPC service changes fault rules without a Corefile reload. Queries outside the configured zones can continue to another CoreDNS plugin, such as `forward`.

The Go package is named `kubernetes`. The Corefile directive is `k8s_dns_chaos`. Each server block accepts the directive once.

## Build the server image

Use Docker with BuildKit to build CoreDNS with this plugin:

```sh
make image
```

The image tag is `ghcr.io/chaos-mesh/chaos-coredns:latest`. The [Dockerfile](Dockerfile) checks out CoreDNS, adds this plugin to `plugin.cfg`, and builds the server with the local source. The image includes `/coredns` and CA certificates.

To extract the executable into the repository root:

```sh
make coredns
```

This target builds the image first and extracts a **Linux** executable for the Docker build platform. It does not produce a native macOS or Windows executable. The target uses the temporary container name `extract-coredns`.

Use `make help` to list build targets. The build toolchain and dependency versions are defined in [go.mod](go.mod) and [Dockerfile](Dockerfile).

## Configure and run DNS

The server needs access to the Kubernetes API. Without an `endpoint` or `kubeconfig` option, it uses in-cluster credentials. Grant access to Services, core/v1 Endpoints, Namespaces, and target Pods. The backend watches Endpoints, not EndpointSlices. The [E2E RBAC manifest](e2e/manifests/chaos-coredns-rbac.yaml) provides a deployment example.

For an in-cluster server, save this configuration as `Corefile`:

```txt
.:53 {
	errors
	ready
	k8s_dns_chaos cluster.local in-addr.arpa ip6.arpa {
		pods insecure
		fallthrough in-addr.arpa ip6.arpa
		ttl 30
		grpcport 9288
	}
	forward . /etc/resolv.conf
}
```

Mount the Corefile into the container and pass `-conf /etc/coredns/Corefile`. See the [E2E Deployment](e2e/manifests/chaos-coredns-deployment.yaml) and [Service](e2e/manifests/chaos-coredns-service.yaml) for DNS and gRPC ports.

For a server outside the cluster, add `kubeconfig /etc/kubernetes/config CONTEXT` inside the plugin block. Mount the kubeconfig and its referenced credential files. This example runs that configuration in Docker:

```sh
docker run --rm --name chaos-coredns \
	-p 1053:53/udp -p 1053:53/tcp -p 127.0.0.1:9288:9288 \
	-v "$PWD/Corefile:/etc/coredns/Corefile:ro" \
	-v "$PWD/kubeconfig:/etc/kubernetes/config:ro" \
	ghcr.io/chaos-mesh/chaos-coredns:latest \
	-conf /etc/coredns/Corefile
```

The API address and credentials must work from inside the container. GCP kubeconfigs must use an external exec credential provider, such as `gke-gcloud-auth-plugin`. The old `auth-provider: gcp` configuration no longer works with the current client-go dependency. The scratch image does not include external credential executables.

Target Pods must send their DNS queries to this server, and the server must see each Pod's source IP. Applying a fault rule does not change a Pod's DNS configuration. The [E2E client Pod](e2e/manifests/test-pod.yaml) uses `dnsPolicy: None` and points `dnsConfig.nameservers` to the chaos DNS Service.

For fault validation, omit the CoreDNS `cache` plugin. A response cache that runs before this plugin can return an earlier answer and hide rule changes. Client-side DNS caches can also retain answers.

## Corefile reference

The parser in [setup.go](setup.go) accepts these options. The fork does not inherit every option from the current upstream `kubernetes` plugin.

```txt
k8s_dns_chaos [ZONES...] {
	endpoint URL
	tls CERT KEY CACERT
	kubeconfig KUBECONFIG CONTEXT
	namespaces NAMESPACE...
	namespace_labels EXPRESSION
	labels EXPRESSION
	pods POD-MODE
	endpoint_pod_names
	ttl TTL
	noendpoints
	transfer to ADDRESS...
	fallthrough [ZONES...]
	ignore empty_service
	chaos ACTION SCOPE NAMESPACE.POD...
	grpcport PORT
}
```

`ZONES` lists the authoritative DNS zones. If omitted, the server block's zones apply. At least one zone must be a forward zone, such as `cluster.local`.

| Option | Behavior |
| --- | --- |
| `endpoint URL` | Connects to the specified Kubernetes API server. If several URLs are supplied, only the first is used. |
| `tls CERT KEY CACERT` | Uses the client certificate, private key, and CA file for the API connection. |
| `kubeconfig KUBECONFIG CONTEXT` | Uses the specified kubeconfig file and context. Both arguments are required. |
| `namespaces NAMESPACE...` | Exposes DNS records for the listed namespaces. |
| `namespace_labels EXPRESSION` | Selects exposed namespaces by label. Cannot be combined with `namespaces`. |
| `labels EXPRESSION` | Filters the Kubernetes objects used by the DNS backend by label. |
| `pods disabled` | Disables Pod IP records. This is the default. |
| `pods insecure` | Answers Pod IP record queries without verifying that the Pod exists. |
| `pods verified` | Answers Pod IP record queries only when the Pod IP exists in the namespace. Maintains a Pod cache. |
| `endpoint_pod_names` | Uses a target Pod name for an endpoint record when the endpoint has no hostname. Otherwise, the fallback is a dashed IP address. |
| `ttl TTL` | Sets normal DNS record TTLs in seconds. The default is `5`, and the accepted range is `0` through `3600`. |
| `noendpoints` | Disables the Endpoints cache and endpoint answers, including headless Service answers. |
| `transfer to ADDRESS...` | Configures the legacy zone-transfer address list. `*` accepts any address. |
| `fallthrough [ZONES...]` | Passes missing records to the next plugin for the listed zones, or all zones when omitted. |
| `ignore empty_service` | Returns `NXDOMAIN` for a normal Service without ready endpoint addresses. |
| `chaos ACTION SCOPE NAMESPACE.POD...` | Configures a static fault for the listed source Pods. At least one Pod is required. |
| `grpcport PORT` | Sets the gRPC TCP port. The default is `9288`. The listener binds on all interfaces and uses plaintext gRPC. |

Namespace and label options control DNS record visibility. The `chaos` targets select the Pods whose requests receive faults. The `pods` option controls Pod DNS records, not whether a Pod can be a fault target.

## Static fault rules

A static target has the form `NAMESPACE.POD`. For example, `busybox.busybox-0` identifies Pod `busybox-0` in namespace `busybox`. The Pod must exist when the server starts so that the plugin can read its IP.

The two actions are:

- `error` returns DNS `SERVFAIL`.
- `random` returns a random IPv4 address for an A query. For an AAAA query, the current implementation returns the fixed address `2001:db8::123:12:1`. Other query types return `SERVFAIL`. Synthetic answers have a TTL of 10 seconds.

The current implementation supports static `all` rules, which affect all queried names. It also parses `inner` and `outer`, intended for names inside and outside the configured zones, but does not initialize their selectors. Those two scopes are unsupported and can panic on a target Pod's request. Use gRPC `patterns` to select names at runtime.

To return errors for all DNS queries from `busybox.busybox-0`, add this line inside the plugin block:

```txt
chaos error all busybox.busybox-0
```

To return synthetic IP addresses instead:

```txt
chaos random all busybox.busybox-0
```

## Change faults through gRPC

The [protobuf schema](pb/dns.proto) defines the legacy `pb.DNS` service. The method paths are `/pb.DNS/SetDNSChaos` and `/pb.DNS/CancelDNSChaos`. Generated Go clients are in [pb/dns.pb.go](pb/dns.pb.go).

`SetDNSChaos` accepts a rule name, an action, target Pods, and optional domain patterns. An empty `patterns` list affects all queried names. With patterns, only matching names receive the fault. Exact names can omit the trailing dot. The implementation uses the TiDB table-rule selector for pattern matching. Put a wildcard at the end, as in `chaos-test.local*`. Leading wildcard patterns such as `*.example.com` are rejected. The request's `scope` and `selector` fields are retained in the schema but are not used by the handler.

For example, send this request through `pb.NewDNSClient(conn).SetDNSChaos`:

```go
&pb.SetDNSChaosRequest{
	Name:     "example-fault",
	Action:   "error",
	Pods:     []*pb.Pod{{Namespace: "busybox", Name: "busybox-0"}},
	Patterns: []string{"example.com", "chaos-test.local*"},
}
```

Call `CancelDNSChaos` with `&pb.CancelDNSChaosRequest{Name: "example-fault"}` to remove that rule. Cancelling an unknown name succeeds. Keep rule names and target Pods distinct when applying concurrent experiments. The legacy handler does not isolate overlapping rules for the same Pod.

Rules installed through gRPC exist in memory and do not survive a server restart. The [gRPC test](grpc_server_test.go) exercises both legacy method paths, fault injection, and recovery with a fake Kubernetes client.

## Develop and test

Use the Go version declared in [go.mod](go.mod). Check the active shared cache locations before installing tools or building:

```sh
go env GOCACHE GOMODCACHE GOPATH
```

Reuse those caches across branches and worktrees. The Dockerfile also uses shared BuildKit cache mounts for module downloads and Go builds.

Run the checks used by [CI](.github/workflows/e2e-tests.yaml):

```sh
go test -race -count=1 ./...
go vet ./...
go mod tidy -diff
go -C e2e test -run '^$' ./...
go -C e2e vet ./...
go -C e2e mod tidy -diff
```

The root tests cover Corefile parsing, DNS records and handlers, informer processing, and the legacy gRPC contract. They use fake Kubernetes clients or test backends. The separate [e2e module](e2e/go.mod) compiles with `-run '^$'` but does not execute its cluster tests in that command.

### Regenerate protobuf code

Install `protoc` through your system package manager, then run:

```sh
make protoc
```

The Makefile resolves `protoc-gen-go` with `go tool -n`. Its version is locked by the root module's `tool` declaration and dependency graph. Do not replace it with an unpinned global generator. Commit schema changes together with regenerated `pb/dns.pb.go`, and preserve the `pb.DNS` service and existing field numbers for client compatibility.

### Run the cluster E2E suite

The suite requires Bash, Go, Docker with BuildKit, minikube, kubectl, and access to the image registries and build dependencies. Local TCP port `9288` must be free.

Run the script on a disposable host or isolated CI runner with a dedicated kubeconfig. The fixed profile `chaos-dns-e2e` must not already belong to another task. The script uses kubectl's current context, deletes that profile on exit, and kills processes matching `port-forward.*chaos-dns-e2e`. A private kubeconfig alone does not isolate that cleanup.

```sh
KUBECONFIG=/absolute/path/to/disposable-kubeconfig ./e2e/e2e.sh
```

The script builds the `e2e-test` image, loads it into minikube, deploys the [test manifests](e2e/manifests), and runs DNS queries from `test-client`. The tests cover baseline cluster DNS, both actions, pattern matching, and cancellation through gRPC. This suite does not run the Chaos Mesh controller or verify a `DNSChaos` custom resource. Its Corefile includes a response cache, so it is not a complete check of fault changes for previously queried names.

## License

[Apache License 2.0](LICENSE).
