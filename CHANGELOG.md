# Changelog

All notable changes to Contaigen will be documented in this file.

The format follows the spirit of Keep a Changelog, and this project uses
semantic version tags.

## [Unreleased]

### Added

- GPLv3 project licensing.
- Release-readiness documentation for v0.1.
- Contributor and security policy documentation.

## [0.1.0] - Pending

### Added

- Cobra-based `contaigen` CLI.
- Docker connectivity checks with `contaigen doctor`.
- Environment profiles for Parrotsec and Kali terminal/desktop workbenches.
- Persistent host-backed workspaces.
- Environment lifecycle commands for create, list, info, start, stop, remove,
  and enter.
- Colorized and progress-aware CLI output for long-running create flows.
- Docker image pull progress aggregation to reduce noisy layer output.
- Network profiles for bridge, isolated, host, segment, and VPN-routed modes.
- OpenVPN sidecar gateways with full and split route modes.
- Split-route OpenVPN handling for static routes and server-pushed lab routes.
- noVNC desktop support through Kasm desktop images.
- VPN sidecar noVNC port publishing with `--vnc`.
- Target service support from built-in templates or arbitrary images.
- Compose validation and attach/detach workflows for segmented environments.
- `.env` file injection for environments, services, and VPN gateways.
- Shell transcript logging for `env enter`.
- Workspace backup, encrypted backup, restore, import, and remove flows.
- `contaigen nuke` for interactive or non-interactive cleanup of managed
  resources.
- GoReleaser-based snapshot and GitHub release automation.
- v0.1 dogfooding runbook and usage documentation.

### Known Issues

- Kali Linux `nmap` packages may fail with `Operation not permitted` in Docker
  containers when the packaged binary has `cap_net_admin` and the container was
  not created with `NET_ADMIN` in its capability bounding set. The v0.1 runtime
  workaround is to create the environment with `--cap-add NET_ADMIN`. A future
  Contaigen-managed Kali image may remove the `cap_net_admin` requirement and
  keep only narrower capabilities such as `cap_net_raw+eip`.
- noVNC desktop images may present a browser TLS warning because the Kasm images
  serve HTTPS with container-local certificates.
- VPN gateway support depends on Docker host support for Linux capabilities and
  `/dev/net/tun`; Docker Desktop platform behavior may vary.

[Unreleased]: https://github.com/NorthernReach/contaigen/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/NorthernReach/contaigen/releases/tag/v0.1.0
