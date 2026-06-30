# ADR-016: SDK ↔ Server SemVer Policy

## Status
Accepted

## Context
Three SDKs (`sdk/go`, `sdk/python`, `sdk/typescript`) live in-repo with independent versioning. Without a shared policy, SDK MAJOR versions can drift from the server, causing silent incompatibilities. The server already sets `X-Hermem-API-Version` on every response (H2.2), but SDKs had no mechanism to detect or react to version mismatches.

## Decision
**`server.MAJOR == sdk.MAJOR`** — all SDKs and the server share the same MAJOR version number.

1. **Version header** — Server sets `X-Hermem-API-Version` on every HTTP response via `APIVersionMiddleware`.
2. **SDK-side detection** — Each SDK reads the header on the first response and compares MAJOR versions:
   - **Go**: `client.OnVersionMismatch func(server, sdk string)` callback, called at most once via `sync.Once`.
   - **Python**: `warnings.warn` by default; `strict=True` raises `APIError`; custom `on_version_mismatch` callback.
   - **TypeScript**: `onVersionMismatch` option in `ClientOptions`, called at most once.
3. **CI enforcement** — `sdk.yml` runs all SDK tests (including version negotiation) on every push/PR and on release tags.
4. **Release gate** — `release.yml` runs a `version-check` job that extracts MAJOR from the release tag, Python `pyproject.toml`, and TypeScript `package.json`. Build fails if any MAJOR differs.
5. **OpenAPI** — `X-Hermem-API-Version` documented as a global response header in the spec.

## Alternatives Considered
- **Independent SDK versioning** — rejected: drift risk, no contract guarantee.
- **Server returns 4xx on MAJOR mismatch** — rejected: too aggressive, breaks existing clients on upgrade.
- **SDK refuses to connect on mismatch** — rejected: same as above.

## Consequences
- MAJOR bumps are atomic: tag, Go module, Python pyproject.toml, TypeScript package.json must all agree.
- Downstream users get a clear warning (or error in strict mode) when SDK and server diverge.
- Release workflow enforces consistency — a mismatched MAJOR blocks the release.
