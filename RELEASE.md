# Release Process

This document describes the release process for Hermem.

## Versioning

Hermem follows Semantic Versioning (SemVer):

- **MAJOR**: Incompatible API changes (server MAJOR must match SDK MAJOR)
- **MINOR**: Backwards-compatible functionality additions
- **PATCH**: Backwards-compatible bug fixes

## Release Steps

### 1. Prepare the Release

1. Ensure all CI checks pass on `main`
2. Update version numbers in:
   - `src/main.go` (via git tags)
   - `sdk/go/client.go` (`SDKVersion`)
   - `sdk/python/pyproject.toml`
   - `sdk/typescript/package.json`
3. Update CHANGELOG.md with release notes

### 2. Create a Release Tag

```bash
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

### 3. Automated Release

Pushing a tag starting with `v` triggers the GitHub Actions release workflow:

1. **Version Check**: Verifies MAJOR version consistency across server and SDKs
2. **Build**: Cross-compiles for:
   - Linux (amd64, arm64)
   - macOS (amd64, arm64)
   - Windows (amd64, arm64)
3. **Code Signing**: Signs macOS binaries (if Apple credentials configured)
4. **Archives**: Creates `.tar.gz` (Unix) and `.zip` (Windows) archives
5. **Checksums**: Generates SHA256 checksums
6. **GitHub Release**: Creates a GitHub release with all artifacts

### 4. SDK Releases

SDKs are released as part of the same tag. The release workflow verifies:

- Go SDK: Module compatibility
- Python SDK: PyPI package
- TypeScript SDK: npm package

## Release Artifacts

Each release includes:

- `hermem-{version}-{os}-{arch}.tar.gz` (Unix binaries)
- `hermem-{version}-{os}-{arch}.zip` (Windows binaries)
- `checksums-sha256.txt` (SHA256 checksums)

## Hotfixes

For critical bugs:

1. Create a branch `hotfix/v0.1.1`
2. Apply the fix
3. Tag and push: `git tag -a v0.1.1 -m "Hotfix v0.1.1"`
4. The release workflow handles the rest

## Rollback

If a release has issues:

1. Mark the GitHub release as "Pre-release"
2. Create a new release with the fix
3. Do not delete tags (they are immutable)
