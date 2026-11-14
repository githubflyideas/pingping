# Security policy

## Supported versions

Only the latest release of pingping gets security fixes. Upgrading is a binary swap;
the data directory stays compatible.

## Reporting a vulnerability

Please report privately, not in a public issue: on GitHub open the
repository's **Security** tab and choose **Report a vulnerability**. Include the
version (`pingping --version`), how it is deployed (binary, service or container,
behind a proxy or not), and steps to reproduce.

You should get a first answer within 7 days. Fixed issues are listed in the
release notes of the version that fixes them, with credit if you want it.

## Scope and known design choices

- pingping does not terminate TLS. Put it behind a reverse proxy with TLS, or bind
  it to localhost, when it is reachable beyond a trusted network.
- Release archives carry SHA-256 checksums (`SHA256SUMS.txt`).
- Dependencies and the Go toolchain are checked with `govulncheck` on every
  push and weekly.
- The console's sponsor slot is the only outbound request besides the probes;
  `--no-sponsor` turns it off.
