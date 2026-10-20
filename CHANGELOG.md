# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Require Go 1.27.3.

## [0.3.0] - 2026-10-20

### Added

- Static DNS rules with domain-to-IP mappings for IPv4 and IPv6.

### Changed

- Replace the forked Kubernetes DNS backend with a standalone `dns_chaos` plugin. Existing deployments must update their Corefile and grant EndpointSlice permissions. See the [upgrade guide](README.md#upgrade-from-the-forked-plugin).
- Require Go 1.27.2 and upgrade CoreDNS to 1.14.7.

### Fixed

- Prevent shared DNS caching from hiding experiments, leaking injected answers to other Pods, or retaining them after cancellation.
- Preserve fault rules across CoreDNS reloads and fix rule updates and cancellation when target Pods or their IPs change.

[Unreleased]: https://github.com/chaos-mesh/k8s_dns_chaos/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/chaos-mesh/k8s_dns_chaos/compare/v0.2.8...v0.3.0
