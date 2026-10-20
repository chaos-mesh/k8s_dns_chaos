# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] - 2026-10-20

### Added

- Static DNS rules with exact domain-to-IP mappings for IPv4 and IPv6. Queries for the other address family return a successful empty answer.
- Unit, race, and cluster E2E tests, including DNSChaos experiments driven by the Chaos Mesh controller.

### Changed

- Replace the forked Kubernetes DNS backend with the standalone `dns_chaos` plugin and the upstream CoreDNS `kubernetes` plugin. Existing deployments must update their Corefile and grant EndpointSlice permissions. See the [upgrade guide](README.md#upgrade-from-the-forked-plugin).
- Require Go 1.27.2 and update CoreDNS to 1.14.7, Kubernetes clients to 0.37.1, and gRPC and protobuf dependencies.
- Regenerate protobuf bindings while preserving the legacy `pb.DNS` service paths, Go `pb` import path, and existing field numbers.
- Require an external exec credential provider for GCP kubeconfigs instead of the removed `auth-provider: gcp` integration. The container image does not include credential provider executables.

### Removed

- The root Go package for the forked DNS backend. The standalone plugin is in the `chaos` package.
- The `k8s_dns_chaos` Corefile directive and static `chaos ACTION SCOPE` configuration. Configure faults through gRPC or Chaos Mesh DNSChaos instead. Non-empty legacy gRPC `scope` and `selector` values are rejected.

### Fixed

- Apply fault rules before the shared DNS cache, so normal cached answers cannot hide experiments and injected answers cannot leak to other Pods or remain cached after cancellation.
- Preserve fault rules and gRPC availability across CoreDNS reloads, including failed reloads and gRPC port changes.
- Refresh target Pod IP addresses and remove stale IP associations when Pods change or disappear.
- Validate rule updates before changing active targets. Same-name updates retain omitted targets, and cancellation removes all targets accumulated under the rule.
- Generate random IPv6 answers for AAAA queries and pass other non-address query types to the next plugin.
- Reject invalid domain patterns, unsupported actions, and conflicting targets before applying rules.

[Unreleased]: https://github.com/chaos-mesh/k8s_dns_chaos/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/chaos-mesh/k8s_dns_chaos/compare/v0.2.8...v0.3.0
