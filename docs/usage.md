# Contaigen Usage Guide

This guide covers the core v0.1 workflows for using Contaigen as a daily
Docker-backed security workbench.

## Concepts

- **Environment**: a named workbench container, similar to a project VM.
- **Workspace**: a host directory mounted into an environment, usually at
  `/workspace`.
- **Network profile**: the networking shape for an environment: `bridge`,
  `isolated`, `host`, `segment`, or `vpn`.
- **VPN gateway**: a sidecar container that owns a VPN connection. VPN-routed
  environments share its network namespace.
- **Service**: a target application container attached to an environment network.
- **Profile**: a reusable environment template, such as `parrot-default` or
  `parrot-desktop`.
- **Service template**: a reusable target app template, such as `juice-shop`.

## Basic Health Checks

Build and inspect the local binary:

```sh
make build
./dist/contaigen version
./dist/contaigen doctor
```

List built-in environment profiles:

```sh
./dist/contaigen profile list
./dist/contaigen profile show parrot-desktop
```

## Terminal Workbench

Create a Parrot environment with a segment network and default workspace:

```sh
./dist/contaigen env create lab --profile parrot-default --network segment
```

Enter it:

```sh
./dist/contaigen env enter lab
```

Run a one-off command:

```sh
./dist/contaigen env enter lab -- /bin/bash -lc 'id && pwd && ls -la /workspace'
```

Inspect it:

```sh
./dist/contaigen env info lab
./dist/contaigen env list
```

## Desktop Workbench

Create a Kasm-backed Parrot desktop:

```sh
./dist/contaigen env create desktop --profile parrot-desktop
```

Inspect the connection details:

```sh
./dist/contaigen env info desktop
```

The displayed noVNC credentials are for the browser login. They do not set the
Linux sudo password inside the container.

## VPN Gateway

Create a split-route OpenVPN sidecar:

```sh
./dist/contaigen vpn create htb --config ~/vpn/htb.ovpn --route-mode split
```

Create a workbench that shares the VPN gateway network namespace:

```sh
./dist/contaigen env create vpn-lab --profile parrot-default --vpn htb
```

For desktop access through a VPN gateway, publish noVNC on the gateway:

```sh
./dist/contaigen vpn create htb-desktop --config ~/vpn/htb.ovpn --route-mode split --vnc
./dist/contaigen env create vpn-desktop --profile parrot-desktop --vpn htb-desktop
```

Inspect routing and logs:

```sh
./dist/contaigen vpn info htb-desktop
./dist/contaigen vpn logs htb-desktop --tail 100
./dist/contaigen env info vpn-desktop
```

In VPN mode, environment ports cannot be published on the workbench container.
Publish them on the VPN gateway instead, because Docker shares the gateway's
network namespace with the workbench.

## Target Services

Create a segmented workbench:

```sh
./dist/contaigen env create appsec --profile parrot-default --network segment
```

Attach a built-in service template:

```sh
./dist/contaigen service add appsec juice-shop
./dist/contaigen service list appsec
```

Attach an arbitrary image:

```sh
./dist/contaigen service add appsec nginx:alpine --name web --alias web.local
```

From inside the workbench, target services are reachable by alias on the segment
network.

## Compose Apps

Validate a Compose file:

```sh
./dist/contaigen compose validate ./compose.yaml
```

Start the Compose app on an environment's segment network:

```sh
./dist/contaigen compose up appsec ./compose.yaml
```

Stop it:

```sh
./dist/contaigen compose down appsec ./compose.yaml
```

## Workspaces And Backups

Create a workspace directly:

```sh
./dist/contaigen workspace create client-a
```

Back it up:

```sh
./dist/contaigen workspace backup client-a
```

Create an encrypted backup:

```sh
./dist/contaigen workspace backup client-a --password-file ./backup.pass
```

Restore a backup into a new workspace:

```sh
./dist/contaigen workspace restore ./client-a.tar.gz.c3enc --name client-a-restored --password-file ./backup.pass
```

Encrypted backups use Contaigen's `.c3enc` wrapper. Generic tar/gzip tools will
not open them directly.

## Environment Variables

Use `.env` files for repeatable non-template configuration:

```sh
./dist/contaigen env create lab --profile parrot-default --env-file ./lab.env
./dist/contaigen service add lab postgres --env-file ./postgres.env
./dist/contaigen vpn create corp --config ./corp.ovpn --env-file ./vpn.env
```

Contaigen reads the file at create time and passes values to Docker. It does not
store a separate secret record.

## Shell Logs

Log command output from an interactive or one-off shell session:

```sh
./dist/contaigen env enter lab --log
./dist/contaigen env enter lab --log-output ./lab-session.log -- /bin/bash -lc 'whoami'
```

Shell logs capture stdout/stderr only. They do not record stdin keystrokes.

## Clean Slate

Preview what would be removed:

```sh
./dist/contaigen nuke --dry-run
```

Run an interactive reset:

```sh
./dist/contaigen nuke
```

Run a non-interactive reset with encrypted workspace backups:

```sh
./dist/contaigen nuke --yes --backup-workspaces --password-file ./backup.pass
```

`nuke` only targets Contaigen-managed resources.
