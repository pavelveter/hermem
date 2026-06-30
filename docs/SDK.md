# Hermem SDK ↔ Server Versioning Policy

## SemVer Contract

**Server MAJOR version == SDK MAJOR version.**

When the server bumps MAJOR (breaking API change), all three SDKs (Go, Python,
TypeScript) must bump MAJOR in the same release. MINOR and PATCH are
independent.

| Server | Go SDK | Python SDK | TS SDK |
|--------|--------|------------|--------|
| 0.3.0  | 0.x    | 0.1.0      | 0.1.0  |

## Version Header

Every response includes `X-Hermem-API-Version: <MAJOR>.<MINOR>.<PATCH>`.
SDKs read this on the first request and compare MAJOR:

- **Go**: `client.OnVersionMismatch func(server, sdk string)`
- **Python**: `warnings.warn` by default; `strict=True` raises
- **TypeScript**: `client.on('versionMismatch', ...)` event

## Release Workflow

1. Bump version in all four places atomically:
   - `Makefile` VERSION (server)
   - `sdk/go/` — no version file (uses go.mod)
   - `sdk/python/pyproject.toml`
   - `sdk/typescript/package.json`
2. Tag: `git tag v<MAJOR>.<MINOR>.<PATCH>`
3. CI verifies SDK compatibility before merge.
