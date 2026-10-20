# Repository guide

This repository builds the `k8s_dns_chaos` CoreDNS plugin used by Chaos Mesh DNSChaos. It contains a fork of the Kubernetes DNS backend. The root Go package is named `kubernetes`, and the Corefile directive is `k8s_dns_chaos`.

## Code map

- `setup.go` registers the plugin, parses Corefile options, and starts the Kubernetes caches and gRPC listener.
- `handler.go` handles DNS requests and chooses between normal records, faults, and the next plugin.
- `kubernetes.go`, `parse.go`, `reverse.go`, `ns.go`, and `xfr.go` implement Kubernetes DNS records and zone transfers.
- `controller.go`, `watch.go`, and `object/` watch and index Kubernetes objects. The current backend uses core/v1 Endpoints.
- `chaos.go` owns fault decisions and synthetic answers. `grpc_server.go` updates the in-memory rule and Pod maps.
- `pb/dns.proto` owns the legacy gRPC contract. `pb/dns.pb.go` is generated.
- Root `*_test.go` files use test backends and fake Kubernetes clients. `e2e/` is a separate Go module with real-cluster tests and manifests.
- `Dockerfile` builds CoreDNS with this plugin. `Makefile` owns image builds, executable extraction, and protobuf generation. `.github/workflows/` defines checks and image publication.

Read [README.md](README.md) for supported options and current runtime limits. Check the implementation before copying an option from upstream CoreDNS. Keep the plugin directive, legacy `pb.DNS` method paths, and protobuf field numbers compatible unless the task explicitly changes those contracts.

## Build and validation

Use the toolchain declared in `go.mod`. The E2E module has its own `go.mod` and `go.sum`, with a local replacement for the root module.

For Go changes, run relevant root tests and the applicable CI checks:

```sh
go test -race -count=1 ./...
go vet ./...
go mod tidy -diff
go -C e2e test -run '^$' ./...
go -C e2e vet ./...
go -C e2e mod tidy -diff
```

The E2E command with `-run '^$'` only compiles tests. Root tests do not prove cluster DNS or Chaos Mesh controller behavior. Report the checks that actually ran.

`make image` builds `ghcr.io/chaos-mesh/chaos-coredns:latest`. `make coredns` extracts a Linux executable using a container named `extract-coredns`. Coordinate use of that fixed name when builds share a Docker daemon.

When editing the schema, run `make protoc` with system `protoc` and review the generated diff. The Makefile uses the module-locked `go tool` generator. Do not hand-edit `pb/dns.pb.go` or install an unpinned replacement generator.

## Caches and runtime ownership

Before installing dependencies or building, inspect existing project conventions and `go env GOCACHE GOMODCACHE GOPATH`. Reuse project caches or the tool defaults across tasks, worktrees, and agents. Re-run Go tests with `-count=1`, not cache deletion. Do not create task-specific dependency caches in temporary evidence directories.

Reuse the Dockerfile's BuildKit caches. Isolate source, test output, and runtime resources rather than dependency downloads. If permissions block a shared cache, find an existing writable shared cache or request access to the specific path. Record the reason, path, and cleanup for any necessary cache exception.

Before running `e2e/e2e.sh`, read its cleanup and confirm ownership of the runtime. It uses the fixed minikube profile `chaos-dns-e2e`, kubectl's current context, TCP port `9288`, and broad port-forward process matching. Use a disposable host or isolated CI runner with a dedicated kubeconfig. A private kubeconfig alone does not isolate the cleanup. Do not run it against a shared profile or another task's cluster. Run cluster mutations only within the task-authorized, owned runtime.

Remove only temporary artifacts, containers, and mounts created by the task after confirming they are unused. Do not clear shared caches, remove unrelated source or backups, or run global Docker prune.

## Contributions

Keep README examples consistent with the current parser and runtime. Use signed-off commits, following the repository's `Signed-off-by` convention. Do not publish images, push changes, or mutate external services unless the task authorizes that action.
