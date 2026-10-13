# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
# Build Docker image
make image

# Build and extract CoreDNS binary to ./coredns
make coredns

# Regenerate protobuf code
make protoc
```

## Testing

Run `make check` for unit tests, race checks, E2E compilation, static analysis, and module consistency.

Run `./e2e/e2e.sh` for the full E2E suite. Install Docker, minikube, kubectl, Helm, and Go first. The script creates a unique Docker-driver minikube profile with containerd and a private kubeconfig, then deletes that profile on exit.

The suite combines direct gRPC tests with real DNSChaos `error` and `random` experiments against Chaos Mesh chart `2.8.4`. The controller tests select two ClusterFirst Pods, check an unaffected control Pod, and verify resolver and DNS recovery after CR deletion. The E2E Corefile disables DNS caching so each query reaches the plugin.

Set `E2E_ARTIFACT_DIR` to retain logs at a chosen path. The default is a temporary directory outside the repository. On failure, the script collects controller, daemon, and DNS logs before deleting its cluster.

For a manually prepared cluster, install the same controller, daemon, and `e2e/manifests` fixtures before running `go test -v ./e2e/...`. Set `E2E_GRPC_ADDRESS` to the forwarded DNS gRPC endpoint. The default is `localhost:9288`.

## Architecture

This is a standalone CoreDNS plugin (`dns_chaos`) for Chaos Mesh's DNSChaos feature. It intercepts DNS requests and applies chaos rules (error/random responses) to targeted pods.

### Plugin Chain Position

The plugin sits **before** the kubernetes plugin in the CoreDNS plugin chain:

```
DNS Request → dns_chaos → (if chaos) → Return error/random
                       → (if no chaos) → kubernetes plugin → DNS Response
```

### Core Components

- **chaos/chaos.go**: Main plugin struct (`DNSChaos`), `ServeDNS` handler that checks chaos rules and applies them
- **chaos/grpc_server.go**: gRPC server exposing `SetDNSChaos` and `CancelDNSChaos` RPCs for runtime configuration
- **chaos/pod_tracker.go**: Tracks pod IPs and refreshes them from the Kubernetes API
- **chaos/setup.go**: CoreDNS plugin registration and Corefile directive parsing
- **pb/dns_chaos.proto**: gRPC service definition

### Key Data Structures

- `chaosMap`: Maps chaos rule names to their configurations
- `podMap`: Maps namespace → pod name → PodInfo (chaos config per pod)
- `ipPodMap`: Maps source IP → PodInfo (for fast lookup during DNS requests)

### Dependencies

- Go v1.26.x
- CoreDNS v1.14.6 with `github.com/coredns/caddy` (forked Caddy v1)
- Kubernetes client-go v0.35.x
- gRPC v1.83.x

## Code Style

- No end-of-line comments
- No Chinese in code comments

## Git Commits

- All commits must include the `-s` / `--signoff` flag (e.g., `git commit -s -m "message"`)
