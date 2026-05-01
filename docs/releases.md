# Releases

Contaigen uses GoReleaser for cross-platform CLI artifacts. The first release
pipeline builds signed-by-checksum archives for macOS, Linux, and Windows on
amd64 and arm64.

## Local Builds

Build the current workspace into `dist/contaigen`:

```sh
make build
```

Run the normal test suite:

```sh
make test
```

Run Docker-backed integration tests when Docker is available:

```sh
make integration-test
```

## Snapshot Artifacts

Use a snapshot to test the release layout without publishing anything:

```sh
make snapshot
```

This installs a pinned GoReleaser binary into `.bin/` on first use, then runs
`.bin/goreleaser release --snapshot --clean`. Artifacts are written under
`dist/`.

To install a different GoReleaser version:

```sh
make snapshot GORELEASER_VERSION=v2.15.4
```

If GoReleaser is installed locally and you prefer the binary on your PATH, run:

```sh
make snapshot GORELEASER=goreleaser
```

## v0.1 Release Candidate Checklist

Before cutting a v0.1 tag or RC:

- Run `gofmt` on changed Go files.
- Run `go test ./...`.
- Run `make release-check`.
- Run `make snapshot`.
- Complete the dogfooding runbook in `docs/dogfooding.md`.
- Confirm the README and `docs/usage.md` match the current CLI behavior.
- Confirm release archives include `LICENSE`, `CHANGELOG.md`, `SECURITY.md`,
  `CONTRIBUTING.md`, and `docs/licensing.md`.
- Confirm the Kali `nmap` capability behavior is documented as a known issue.

## Release Tags

Public releases are tag-driven. Push a semantic version tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The `release` GitHub Actions workflow runs unit tests, builds platform archives,
generates checksums, and publishes a GitHub Release.

## Version Metadata

Both `make build` and GoReleaser inject build metadata into the CLI:

```text
main.version
main.commit
main.date
```

Users can inspect that metadata with:

```sh
contaigen version
```

## Current Distribution Scope

Included now:

- GitHub Release archives for `darwin`, `linux`, and `windows`.
- `amd64` and `arm64` binaries.
- Checksums for release artifacts.
- CI validation for unit tests and GoReleaser config.

Deferred:

- Homebrew, Scoop, Winget, apt, rpm, and container image publishing.
- Artifact signing with GPG or Sigstore.
- SBOM generation.
- Release notes policy beyond GoReleaser's generated changelog.
