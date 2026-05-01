# Contributing

Thanks for helping make Contaigen better.

Contaigen is a CLI-first Docker orchestration tool for security workbenches. The
project values practical security-engineering workflows, clear command behavior,
small focused changes, and documentation that explains real tradeoffs.

## Licensing

Contaigen is licensed under GPLv3. By contributing, you agree that your
contribution is provided under the same GPLv3 license used by the project.

See `LICENSE` and `docs/licensing.md` for the current license summary.

Do not contribute code, templates, images, docs, or generated artifacts unless
you have the right to license them under GPLv3.

## Development Setup

Requirements:

- Go 1.26.2 or newer, matching `go.mod`.
- Docker Engine or Docker Desktop for Docker-backed workflows.

Run the normal checks:

```sh
go test ./...
make release-check
```

Build a local binary:

```sh
make build
./dist/contaigen version
./dist/contaigen doctor
```

Build release-like local artifacts:

```sh
make snapshot
```

Docker integration tests are opt-in:

```sh
CONTAIGEN_INTEGRATION=1 go test ./internal/dockerx -run Integration -count=1
```

## Contribution Guidelines

- Keep changes focused and easy to review.
- Prefer existing package boundaries and command patterns.
- Keep Docker SDK calls behind `internal/dockerx`.
- Keep orchestration decisions in `internal/engine`.
- Keep domain data in `internal/model`.
- Avoid broad refactors unless they directly reduce release risk or user
  confusion.
- Add comments only where they explain non-obvious behavior.
- Update README or docs when user-visible behavior changes.
- Do not print secrets, VPN credentials, `.env` values, or backup passwords in
  command output or tests.
- Treat privileged mode, host networking, device mounts, Linux capabilities,
  Docker socket mounts, and host bind mounts as sensitive behavior.

## Tests

For most changes, run:

```sh
go test ./...
```

For release or packaging changes, also run:

```sh
make release-check
make snapshot
```

For Docker runtime changes, run the integration tests if Docker is available:

```sh
make integration-test
```

## Templates And Examples

Templates should avoid embedded customer data and secrets. Prefer `.env` files
for runtime values, and document any required environment variables.

When adding a template:

- Run `contaigen template validate <path>`.
- Prefer segment networking for app-testing targets.
- Add Linux capabilities only when the image actually needs them.
- Document any privileged or platform-specific requirements.

## Security Reports

Do not report security vulnerabilities in public issues. Follow `SECURITY.md`
instead.
