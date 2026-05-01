# Security Policy

Contaigen is security tooling for creating Docker-backed workbenches. Security
reports are welcome and should be handled carefully.

## Supported Versions

Until v0.1 is released, only the current `main` branch is supported.

After v0.1, security fixes will target the latest released minor version unless
the project states otherwise.

## Reporting A Vulnerability

Please do not open a public issue for a vulnerability that could put users at
risk.

Report security concerns privately to the project maintainer. If a GitHub
Security Advisory workflow is enabled for the repository, use that. Otherwise,
contact the maintainer directly through the contact method published on the
GitHub project or maintainer profile.

Include as much of the following as possible:

- Affected Contaigen version or commit.
- Host operating system and Docker version.
- The command or workflow that triggers the issue.
- Expected behavior and actual behavior.
- Reproduction steps using non-sensitive data.
- Logs or screenshots with secrets removed.
- Whether the issue requires local access, Docker daemon access, malicious image
  control, a crafted template, or a crafted backup/archive.

## Scope

Security reports that are in scope:

- Unsafe handling of workspace backups, restores, or encrypted backups.
- Secret leakage through logs, command output, labels, or release artifacts.
- Incorrect cleanup that removes resources not managed by Contaigen.
- Template, `.env`, VPN config, or archive parsing behavior that can cause
  unintended host file access or command execution.
- Docker orchestration behavior that grants broader privileges than requested.
- Vulnerabilities in Contaigen release artifacts or update/distribution
  metadata.

Reports that are usually out of scope:

- Vulnerabilities in third-party Docker images launched by the user.
- Findings that require intentionally running malicious containers with broad
  host privileges.
- General Docker isolation limitations that Contaigen clearly documents.
- Issues in Kali, Parrotsec, Kasm, OpenVPN images, Docker Desktop, or Docker
  Engine unless Contaigen materially worsens the risk.

## Security Model Notes

Contaigen does not make Docker containers equivalent to virtual machines. Users
should treat containers as useful isolation boundaries with known limitations,
especially when running untrusted tools, malware samples, vulnerable target
applications, VPN clients, or desktop images.

Sensitive operations such as `--privileged`, host networking, device mounts,
Linux capabilities, Docker socket mounts, and host bind mounts should be treated
as high-impact changes.

## Known v0.1 Security-Relevant Limitation

Kali Linux `nmap` packages may require `NET_ADMIN` in the container capability
bounding set because of the capabilities set by Kali package scripts. The v0.1
workaround is `env create --cap-add NET_ADMIN`. This is documented as a known
issue and does not block v0.1, but users should grant capabilities only to
workbenches that actually need them.
