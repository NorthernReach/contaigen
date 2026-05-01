# Integration Tests

Contaigen's default test suite is unit-focused and does not require Docker.
Docker-backed tests are opt-in because they pull images, create containers, and
need access to the local Docker daemon.

Run the normal suite:

```sh
go test ./...
```

Run the Docker integration suite:

```sh
CONTAIGEN_INTEGRATION=1 go test ./internal/dockerx -run Integration -count=1
```

The first integration test uses `testcontainers-go` to pull and start
`alpine:3.20` as a probe. After that, Contaigen's Docker runtime creates a real
managed environment from the same image, starts it, runs a command with Docker
exec, inspects labels/state, and removes the environment.

Requirements:

- Docker must be running and reachable from the current shell.
- The test may pull `alpine:3.20`.
- The test creates temporary containers and removes them during cleanup.

If a run is interrupted, clean up leftover integration resources with Docker:

```sh
docker ps -a --filter label=io.contaigen.integration=testcontainers-probe
docker ps -a --filter label=io.contaigen.managed=true --filter name=contaigen-it-
```
