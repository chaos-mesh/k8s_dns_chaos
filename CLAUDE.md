# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
# Build Docker image
make image

# Build and extract CoreDNS binary to ./coredns
make coredns

# Regenerate protobuf code (uses old-style protoc-gen-go for gRPC v1.29.1 compatibility)
make protoc
```

## Testing

E2E tests require a running Kubernetes cluster with chaos-coredns deployed. Tests use `kubectl exec` to run `nslookup` commands inside a test pod:

```bash
cd e2e && go test -v ./...
```

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
- **pb/dns.proto**: gRPC service definition

### Key Data Structures

- `chaosMap`: Maps chaos rule names to their configurations
- `podMap`: Maps namespace → pod name → PodInfo (chaos config per pod)
- `ipPodMap`: Maps source IP → PodInfo (for fast lookup during DNS requests)

### Dependency Constraints

The Dockerfile pins `google.golang.org/grpc@v1.29.1` because CoreDNS v1.7.0's etcd plugin requires the deprecated `grpc/naming` package. The `clouddns` and `route53` plugins are removed to avoid cloud.google.com module conflicts.

## Code Style

- No end-of-line comments
- No Chinese in code comments
