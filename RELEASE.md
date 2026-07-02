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

## Supply-Chain Security (SLSA Provenance)

Every release artifact is accompanied by a **SLSA Build Level 2**
provenance attestation, generated natively by GitHub Actions via
[`actions/attest-build-provenance`](https://github.com/actions/attest-build-provenance).
The attestation is cryptographically signed (OIDC → Sigstore) and
recorded against the GitHub-native attestations API, where it
appears in the release UI as a **"Verified"** badge on each asset.

Provenance lets a consumer verify that the artifact was genuinely
built by this repository's CI on the tagged commit, and that it has
not been tampered with after the build. Verifiable with the
[GitHub CLI](https://cli.github.com/):

```bash
# Verify a single release asset
gh attestation verify hermem-v0.2.0-linux-amd64.tar.gz \
  --owner pavelveter

# Or, equivalently, via the `gh release` view:
gh release view v0.2.0 --repo pavelveter/hermem \
  --json verification --jq '.verification'
```

**Why `actions/attest-build-provenance` and not the third-party
[`slsa-github-generator`](https://github.com/slsa-framework/slsa-github-generator)?
Both reach the same SLSA Build L2 guarantee (ephemeral GH-hosted
runner + OIDC-signed envelope). The GH-native action is simpler
(no reusable workflow to pin), ships the signed envelope directly
to the GH attestations API (so consumers use `gh attestation` /
the "Verified" UI badge rather than chasing `.intoto.jsonl` files),
and avoids the third-party dependency. The build matrix still
runs on `ubuntu-latest` / `macos-latest` as before; the
provenance job only depends on the build and checksums jobs
completing successfully.

SLSA Build **L3** is **not** feasible on standard GH-hosted
runners (L3 requires a pre-hardened builder). Track the GH-hosted
hardened runner GA for the upgrade path.

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
