## Why

The public package split is complete, but the deprecated `src/internal/core`
facade still keeps duplicate domain, provider, transport, and policy contracts
alive. A coordinated major release is now needed to remove that compatibility
surface without mixing in unrelated vector, tenancy, ingestion, or retrieval
behavior changes.

## What Changes

- **BREAKING** Remove `src/internal/core` aliases, legacy capability interfaces,
  transport DTO aliases, and compatibility-only adapters.
- Migrate every production caller, test fixture, benchmark, CLI adapter, and
  MCP adapter to `pkg/domain`, `pkg/spi`, `api/v1`, or an owning internal
  package before deletion.
- Complete the ADR-030, ADR-032, ADR-035, and ADR-037 dependency gates required
  to remove core-owned tenancy, ingestion, identity, and retrieval contracts.
- Preserve current HTTP routes, JSON envelopes, MCP tool behavior, CLI output,
  persistence behavior, and provider behavior through the removal.
- Add a clean-checkout guard that fails if `src/internal/core` exists or is
  imported after the breaking migration.
- Align server and SDK major versions, publish migration examples and release
  notes, and retain the previous compatibility release as the rollback target.

## Capabilities

### New Capabilities

- `core-facade-removal`: Defines the breaking-release contract, migration
  gates, zero-legacy-dependency invariant, behavior-preservation requirements,
  and release/rollback criteria for removing the deprecated facade.

### Modified Capabilities

None. The existing OpenSpec changes are implementation history rather than
main capability specifications; this change formalizes the release boundary
for an already-approved public package migration.

## Impact

- **Go packages:** `src/internal/core`, internal services and repositories,
  provider adapters, application wiring, CLI/MCP adapters, tests, fixtures,
  and benchmarks.
- **Public contracts:** `pkg/domain`, `pkg/spi`, and `api/v1` become the only
  supported replacement surfaces.
- **Release:** server and SDK MAJOR versions must be aligned; the release
  requires migration notes, API snapshots, checksums, provenance, and a
  supported previous compatibility branch.
- **Dependencies:** ADR-030/032/035/037 completion is a prerequisite; sqlite-
  vec/HNSW/remote backend work and database/vector storage migrations remain
  separate.
- **Compatibility:** source compatibility for `src/internal/core` is removed;
  HTTP/MCP/CLI/persistence/provider runtime behavior remains compatible.
