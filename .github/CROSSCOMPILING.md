# Cross-Compilation & CGO

hermem uses `github.com/mattn/go-sqlite3` which requires CGO. This document describes how cross-compilation works in CI.

## Approach: Zig CC

Zig ships a C/C++ compiler that can target musl Linux, macOS, and Windows MinGW — all from a single Ubuntu runner. No Docker, no osxcross, no manual SDK setup.

### Targets

| GOOS   | GOARCH | Runner     | CC                                      |
|--------|--------|------------|------------------------------------------|
| linux  | amd64  | ubuntu     | `zig cc -target x86_64-linux-musl`       |
| linux  | arm64  | ubuntu     | `zig cc -target aarch64-linux-musl`      |
| darwin | amd64  | macos      | native (no CC override)                  |
| darwin | arm64  | macos      | native (no CC override)                  |
| windows| amd64  | ubuntu     | `zig cc -target x86_64-windows-gnu`      |
| windows| arm64  | ubuntu     | `zig cc -target aarch64-windows-gnu`     |

Darwin builds run on macOS runners natively to avoid cross-SDK issues and to allow code signing/notarization.

## Reproducibility

- `go build -trimpath` removes absolute paths from binaries
- `-ldflags="-s -w"` strips debug symbols for smaller, more deterministic output
- All builds inject the same metadata via ldflags:
  - `main.version` — git tag or `dev`
  - `main.buildDate` — UTC ISO-8601 timestamp
  - `main.gitCommit` — short SHA from `git rev-parse --short HEAD`

## Checksums

Release builds generate `checksums-sha256.txt` with SHA-256 hashes for every published artifact.

```bash
sha256sum -c checksums-sha256.txt
```

## macOS Signing & Notarization

Optional. Requires these GitHub secrets:

- `APPLE_CERTIFICATE` — base64-encoded .p12 Developer ID certificate
- `APPLE_CERTIFICATE_PWD` — certificate password
- `APPLE_ID` — Apple ID email
- `APPLE_TEAM_ID` — Team ID
- `APPLE_APP_PASSWORD` — app-specific password for notarization

If secrets are not configured, signing and notarization are silently skipped.

## Enabling CI

Rename `.disable-ci-github` to `.github`:

```bash
git mv .disable-ci-github .github
```

Workflows will trigger on the next push to `main` or tag push matching `v*`.
