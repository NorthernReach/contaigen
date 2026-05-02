# Contaigen

Contaigen is a CLI-first orchestration tool for Docker-based security
workbenches. It helps security engineers create repeatable Parrotsec, Kali,
VPN, desktop, workspace, and target-service container setups without
hand-building Docker wiring every time.

The project is intentionally command-line first. The goal is to make a
containerized workbench feel close to a daily VM: named environments, persistent
workspaces, controlled networking, VPN sidecars, noVNC desktops, target services,
logs, backups, and templates.

## Current Status

Contaigen v0.1.0 is the first public release. It is ready for early users who
are comfortable with Docker-backed security tooling and command-line workflows.
The v0.1 dogfooding run validated split-route OpenVPN, VPN-routed Parrot
desktop access, segmented target services, workspace persistence, shell logging,
encrypted backup/restore, and release artifacts.

## Features

- Named Docker-backed workbench environments.
- Built-in Parrot and Kali terminal and desktop profiles.
- Host-backed workspaces mounted into environments.
- Network profiles for bridge, isolated, host, segment, and VPN-routed modes.
- OpenVPN sidecar gateways with full or split route modes.
- noVNC desktop support through Kasm images.
- Target services from arbitrary images or reusable service templates.
- Compose app attach flow for segmented testing networks.
- `.env` file injection for environments, services, and VPN gateways.
- Workspace backup, encrypted backup, restore, and import.
- Shell transcript logging for `env enter`.
- Clean-slate workspace resetting with `contaigen nuke`.

## Requirements

- Go 1.26.2 or newer, matching `go.mod`.
- Docker Engine or Docker Desktop.
- For VPN gateways, a Docker host that supports the required VPN container
  capabilities and `/dev/net/tun` device mapping.

## Install

Download a release archive from GitHub, extract it, and place the `contaigen`
binary somewhere on your `PATH`.

macOS release artifacts are not signed or notarized yet. After verifying the
release checksum, macOS users may need to remove the quarantine flag:

```sh
xattr -d com.apple.quarantine ./contaigen
chmod +x ./contaigen
./contaigen version
```

## Build

Run the normal test suite:

```sh
go test ./...
```

Build a local binary:

```sh
make build
```

Inspect build metadata:

```sh
./dist/contaigen version
```

Check Docker connectivity:

```sh
./dist/contaigen doctor
```

## Quick Start

List built-in profiles:

```sh
./dist/contaigen profile list
```

Create a Parrot terminal workbench with a persistent workspace:

```sh
./dist/contaigen env create lab --profile parrot-default --network segment
./dist/contaigen env enter lab
```

Create a browser-accessible Parrot desktop:

```sh
./dist/contaigen env create desktop --profile parrot-desktop
./dist/contaigen env info desktop
```

Add a target service to a segmented environment:

```sh
./dist/contaigen service add lab juice-shop
./dist/contaigen service list lab
```

Create an encrypted workspace backup:

```sh
./dist/contaigen workspace backup lab --password-file ./backup.pass
```

Preview a clean-slate reset:

```sh
./dist/contaigen nuke --dry-run
```

## VPN Desktop Example

Create a split-route OpenVPN sidecar and publish the noVNC port on the sidecar:

```sh
./dist/contaigen vpn create htb --config ~/vpn/htb.ovpn --route-mode split --vnc
```

Create a Parrot desktop environment sharing the VPN gateway network namespace:

```sh
./dist/contaigen env create htb-parrot --profile parrot-desktop --vpn htb
```

Inspect connection details:

```sh
./dist/contaigen env info htb-parrot
./dist/contaigen vpn info htb
```

For VPN-routed desktop environments, ports must be published on the VPN gateway
because the workbench shares the sidecar network namespace.

## Documentation

- [Changelog](CHANGELOG.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Usage guide](docs/usage.md)
- [Template authoring](docs/templates.md)
- [Integration tests](docs/integration-tests.md)
- [Release process](docs/releases.md)
- [Project plan](docs/contaigen-plan.md)

## Development

Run Docker-backed integration tests when Docker is available:

```sh
CONTAIGEN_INTEGRATION=1 go test ./internal/dockerx -run Integration -count=1
```

Validate the release configuration:

```sh
make release-check
```

Build release-like local artifacts without publishing:

```sh
make snapshot
```

`make snapshot` installs a pinned GoReleaser binary into `.bin/` on first use and
writes archives/checksums under `dist/`.

## Safety Notes

- Contaigen only removes resources labeled and managed by Contaigen.
- `contaigen nuke` is intentionally destructive and prompts before removal.
- Encrypted backups use Contaigen's `.tar.gz.c3enc` wrapper and should be
  restored with Contaigen.
- noVNC passwords are browser-session credentials, not Linux sudo passwords.
- Prefer `--password-file` over `--password` for backup passwords.
- Kali `nmap` may require `env create --cap-add NET_ADMIN`; this is documented
  as a known v0.1 issue in [CHANGELOG.md](CHANGELOG.md).

## License

Contaigen is licensed under the GNU General Public License v3.0. See
[LICENSE](LICENSE) and [docs/licensing.md](docs/licensing.md).
