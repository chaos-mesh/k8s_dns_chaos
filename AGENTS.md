# Repository guide

This repository builds the standalone `dns_chaos` CoreDNS plugin used by Chaos Mesh DNSChaos. `cmd/coredns` registers it immediately before `cache`, ahead of the standard CoreDNS `kubernetes` plugin. Fault responses bypass the shared cache. Unmatched requests retain normal caching. The root module includes both the plugin and E2E tests.

## Code map

- `chaos/setup.go` registers `dns_chaos`, parses `grpcport` and `kubeconfig`, and configures the Kubernetes client.
- `chaos/chaos.go` handles DNS requests and owns the shared `chaosRuntime` maps for rules, Pods, and source IPs.
- `chaos/grpc_server.go` validates and applies rule updates through the legacy `pb.DNS` service. Same-name updates accumulate targets.
- `chaos/domain_matcher.go` matches normalized domain patterns. `chaos/pod_tracker.go` refreshes target Pod IPs.
- `chaos/lifecycle.go` shares the runtime and gRPC listener across CoreDNS reloads. `chaos/reload_test.go` covers reload success, rollback, and port changes.
- `pb/dns_chaos.proto` owns the gRPC contract. The `.pb.go` files in `pb/` are generated.
- `chaos/*_test.go` uses fake Kubernetes clients and real local DNS or gRPC listeners. `cmd/coredns` tests verify plugin ordering and cache isolation through the registered handlers.
- `e2e/` contains direct gRPC tests and real Chaos Mesh controller tests. `e2e/manifests/` provides the DNS deployment and client fixtures.
- `Dockerfile` builds `cmd/coredns` directly. `Makefile` owns local builds, checks, and protobuf generation. `.github/workflows/` defines checks and image publication.

Read [README.md](README.md) for supported options and runtime contracts. Keep the legacy `pb.DNS` method paths and protobuf field numbers compatible. Normal cluster records come from the upstream `kubernetes` plugin, which requires EndpointSlice permissions.

## Build and validation

Use the toolchain declared in `go.mod`. Run `make check` for unit and race tests, E2E compilation, static analysis, and module consistency. There is no separate E2E Go module.

The compilation check does not execute cluster tests. Unit tests do not prove cluster DNS or Chaos Mesh controller behavior. Report the checks that actually ran.

`make image` builds `ghcr.io/chaos-mesh/chaos-coredns:latest`. `make coredns` builds a native executable directly from `cmd/coredns` with `CGO_ENABLED=0`.

When editing the schema, run `make protoc` with system `protoc` and review the generated diff. The Makefile uses module-locked `go tool` generators. Do not hand-edit generated `.pb.go` files or install unpinned replacement generators.

## Caches and runtime ownership

Before installing dependencies or building, inspect existing project conventions and `go env GOCACHE GOMODCACHE GOPATH`. Reuse project caches or the tool defaults across tasks, worktrees, and agents. Re-run Go tests with `-count=1`, not cache deletion. Do not create task-specific dependency caches in temporary evidence directories.

Reuse the Dockerfile's BuildKit caches. Isolate source, test output, and runtime resources rather than dependency downloads. If permissions block a shared cache, find an existing writable shared cache or request access to the specific path. Record the reason, path, and cleanup for any necessary cache exception.

Before running `e2e/e2e.sh`, read its cleanup and confirm ownership of the runtime. It creates a unique Docker-driver minikube profile with containerd, a private kubeconfig, and a dynamic local gRPC port. It collects diagnostics before teardown and deletes only its profile and owned port-forward process. Run cluster mutations only within the task-authorized, owned runtime.

The E2E suite requires Docker, minikube, kubectl, Helm, and Go. It installs Chaos Mesh `2.8.5` and tests direct gRPC calls plus real `DNSChaos` experiments. Set `E2E_ARTIFACT_DIR` to retain logs at a chosen path. The default is a temporary directory outside the repository.

Remove only temporary artifacts, containers, and mounts created by the task after confirming they are unused. Do not clear shared caches, remove unrelated source or backups, or run global Docker prune.

## Contributions

Keep README examples consistent with the parser and runtime. Use signed-off commits, following the repository's `Signed-off-by` convention. Do not publish images, push changes, or mutate external services unless the task authorizes that action.
