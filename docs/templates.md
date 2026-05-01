# Contaigen Template Authoring

Contaigen templates are YAML files that make common workbench and target-app
setups repeatable.

There are two template kinds:

- `EnvironmentProfile`: defaults for `contaigen env create --profile <name>`
- `ServiceTemplate`: reusable target containers for `contaigen service add <env> <name>`

Validate any template before using it:

```sh
contaigen template validate ./profiles/parrot-web.yaml
contaigen template validate ./services/juice-shop.yaml
```

Contaigen rejects unknown YAML fields, so validation catches misspellings before
Docker resources are created.

## Where Templates Live

Built-in templates ship inside Contaigen. User templates can be loaded by direct
path, or by name from the configured template directory:

- Linux default: `~/.local/share/contaigen/templates`
- macOS default: `~/Library/Application Support/contaigen/templates`
- Windows default: `%LOCALAPPDATA%\contaigen\templates`

User templates may live directly in that directory or in kind-specific
subdirectories:

```text
templates/
  profiles/
    parrot-web.yaml
  services/
    postgres.yaml
```

User templates shadow built-ins with the same `metadata.name`, which lets teams
keep local defaults while still using Contaigen's bundled templates.

## Environment Profiles

Environment profiles describe workbench containers: image, shell, network,
workspace mount, desktop settings, environment variables, volumes, ports, and
Linux capabilities.

Built-in Parrot profiles:

- `parrot-default`: official `parrotsec/security` terminal workbench.
- `parrot-desktop`: Kasm `kasmweb/parrotos-6-desktop:1.18.0` noVNC desktop
  workbench exposed on `https://127.0.0.1:6901/`; Contaigen creates it with
  Docker `--user root`.

Minimal profile:

```yaml
apiVersion: contaigen.io/v1alpha1
kind: EnvironmentProfile
metadata:
  name: parrot-web
  description: Parrot workbench for web application testing
spec:
  image: parrotsec/security
  shell: /bin/bash
  network:
    profile: segment
    name: client-a
  workspace:
    mountPath: /workspace
  env:
    - TZ=UTC
  capAdd:
    - NET_ADMIN
  pull: true
  start: true
```

Use it:

```sh
contaigen env create web-lab --profile parrot-web
contaigen env create web-lab --profile ./profiles/parrot-web.yaml
```

CLI flags override scalar profile defaults when explicitly supplied, and list
fields such as `env`, `ports`, `volumes`, and `capAdd` are appended.

### Environment Profile Fields

Required:

- `apiVersion`: must be `contaigen.io/v1alpha1`
- `kind`: must be `EnvironmentProfile`
- `metadata.name`: profile name used by `--profile`
- `spec.image`: Docker image for the workbench

Common optional fields:

- `metadata.description`: shown in `contaigen profile list`
- `spec.shell`: default shell for `env enter`; defaults to `/bin/bash`
- `spec.user`: container user, equivalent to `docker run --user`
- `spec.hostname`: container hostname
- `spec.workingDir`: default working directory inside the container
- `spec.pull`: pull the image before create
- `spec.start`: start the container after create

Network:

```yaml
spec:
  network:
    profile: bridge   # bridge, isolated, host, segment, or vpn
    name: client-a    # segment network name, or VPN gateway name for vpn
```

Workspace:

```yaml
spec:
  workspace:
    name: client-a
    path: /host/path/client-a
    mountPath: /workspace
    disabled: false
```

Desktop/noVNC:

```yaml
spec:
  desktop:
    enabled: true
    protocol: novnc
    hostIP: 127.0.0.1
    hostPort: "6901"
    containerPort: "6901"
    scheme: https
    path: /
    user: kasm_user
    passwordEnv: VNC_PW
```

`passwordEnv` controls the noVNC login password for the browser session. It
does not set the Linux user's sudo password inside the container.

Ports and volumes:

```yaml
spec:
  ports:
    - hostIP: 127.0.0.1
      hostPort: "8080"
      containerPort: "80"
      protocol: tcp
  volumes:
    - source: /host/tools
      target: /opt/tools
      readOnly: true
```

Capabilities:

```yaml
spec:
  capAdd:
    - NET_ADMIN
```

## Service Templates

Service templates describe target application containers attached to a
workbench's network. They are useful for application security testing targets
such as Juice Shop, WebGoat, DVWA, databases, or local app containers.

Minimal service template:

```yaml
apiVersion: contaigen.io/v1alpha1
kind: ServiceTemplate
metadata:
  name: postgres
  description: PostgreSQL target database
spec:
  image: postgres:16
  alias: db
  env:
    - POSTGRES_DB=app
    - POSTGRES_USER=app
  ports:
    - hostIP: 127.0.0.1
      hostPort: "5432"
      containerPort: "5432"
      protocol: tcp
  volumes:
    - source: /host/data/postgres
      target: /var/lib/postgresql/data
  pull: true
  start: true
```

Use it:

```sh
contaigen service add lab postgres --env-file ./postgres.env
contaigen service add lab ./services/postgres.yaml --env POSTGRES_PASSWORD=secret
```

Service templates work best with environments created on a segment network:

```sh
contaigen env create lab --profile parrot-default --network segment
contaigen service add lab juice-shop
```

### Service Template Fields

Required:

- `apiVersion`: must be `contaigen.io/v1alpha1`
- `kind`: must be `ServiceTemplate`
- `metadata.name`: template name used by `service add`
- `spec.image`: Docker image for the target service

Optional:

- `metadata.description`: shown in `contaigen service template list`
- `spec.name`: container service name; defaults to the template name
- `spec.alias`: DNS alias on the environment network
- `spec.env`: `KEY=VALUE` environment entries
- `spec.ports`: published ports
- `spec.volumes`: bind mounts
- `spec.command`: command/arguments for the container
- `spec.pull`: pull the image before create
- `spec.start`: start the container after create

Command example:

```yaml
spec:
  image: python:3.12-alpine
  alias: api
  command:
    - python
    - -m
    - http.server
    - "8000"
```

## Handling Secrets

Do not put customer secrets directly in reusable templates. Prefer runtime
inputs:

```sh
contaigen env create lab --profile parrot-web --env-file ./lab.env
contaigen service add lab postgres --env-file ./postgres.env
contaigen vpn create corp --config ./corp.ovpn --env-file ./vpn.env
```

Users can populate those `.env` files with 1Password, shell scripts, CI secrets,
or any other external workflow before running Contaigen.

## Validation Checklist

Before sharing a template:

- Run `contaigen template validate <path>`
- Run `contaigen profile show <name-or-path>` or `contaigen service template show <name-or-path>`
- Avoid embedding customer secrets
- Prefer `network.profile: segment` for app-testing services
- Add `capAdd: [NET_ADMIN]` only when the tool image really needs it
- Keep host paths team-portable where possible
