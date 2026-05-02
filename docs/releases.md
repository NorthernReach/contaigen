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

## Release Candidate Procedure

Use release candidates when you want GitHub to build real artifacts without
declaring the final release stable yet.

1. Confirm the worktree is clean:

   ```sh
   git status --short
   ```

2. Run the release gates:

   ```sh
   go test ./...
   make release-check
   make snapshot
   ```

3. Push the release commit to the default branch before tagging:

   ```sh
   git push origin main
   ```

4. Create and push an annotated RC tag:

   ```sh
   git tag -a v0.1.0-rc.1 -m "Release v0.1.0-rc.1"
   git push origin v0.1.0-rc.1
   ```

5. Wait for the `release` GitHub Actions workflow to complete.

6. Download the generated artifacts from the GitHub Release and smoke test them:

   ```sh
   ./contaigen version
   ./contaigen profile list
   ./contaigen doctor
   ```

7. If the RC has problems, commit fixes and create the next RC tag, such as
   `v0.1.0-rc.2`. Prefer a new RC tag over rewriting a published RC tag.

## Standard Release Procedure

Use this flow after an RC has passed validation.

1. Finalize release-facing docs:

   - Set the `CHANGELOG.md` release date.
   - Update the README status if needed.
   - Confirm known issues are documented.

2. Run the release gates:

   ```sh
   go test ./...
   make release-check
   make snapshot
   ```

3. Commit and push the final release-prep changes:

   ```sh
   git add CHANGELOG.md README.md docs/releases.md
   git commit -m "docs: finalize v0.1.0 release"
   git push origin main
   ```

4. Create and push the final annotated release tag:

   ```sh
   git tag -a v0.1.0 -m "Release v0.1.0"
   git push origin v0.1.0
   ```

5. Wait for the `release` GitHub Actions workflow to complete.

6. Verify the GitHub Release includes:

   - macOS, Linux, and Windows archives.
   - Checksums file.
   - `LICENSE`, `CHANGELOG.md`, `SECURITY.md`, `CONTRIBUTING.md`, README, and
     docs in each archive.

7. Download at least one final artifact, verify its checksum, and smoke test:

   ```sh
   ./contaigen version
   ./contaigen profile list
   ./contaigen doctor
   ```

## Release Tags

Public releases are tag-driven. Push a semantic version tag:

```sh
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

The `release` GitHub Actions workflow runs unit tests, builds platform archives,
generates checksums, and publishes a GitHub Release.

RC tags use prerelease semantic versions such as `v0.1.0-rc.1`. Final release
tags use versions such as `v0.1.0`.

Always tag the exact commit you want to release. Tags point at commits, not at
uncommitted local changes.

## macOS Gatekeeper

The v0.1 macOS archives are not signed or notarized. Users may see this
Gatekeeper message when running the downloaded binary:

```text
Apple could not verify "contaigen" is free of malware that may harm your Mac or
compromise your privacy.
```

After verifying checksums from the release, remove the quarantine flag:

```sh
xattr -d com.apple.quarantine ./contaigen
chmod +x ./contaigen
```

Future release work should add Developer ID signing and notarization.

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
