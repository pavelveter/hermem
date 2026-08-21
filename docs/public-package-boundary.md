# Public package boundary gates

This compatibility release freezes `pkg/domain`, `pkg/spi`, and `api/v1`.

## Required checks

- `pkg/domain` imports only the standard library.
- `pkg/spi` imports `pkg/domain`, never `internal/*` or `api/*`.
- `api/v1` imports `pkg/domain`, never `src/internal/core`.
- Production code may use `core` only through the existing compatibility
  boundary; new public capabilities are added to `pkg/domain` or `pkg/spi`.
- `core.VectorIndex` remains IDs-only and is not extended. Its adapter is
  retained until external and in-repository legacy callers have migrated.
- sqlite-vec, HNSW, remote backends, vector BLOB removal, and rollback
  procedures are explicitly out of scope and remain owned by their dedicated
  ADR changes.

## Release gate

The `core` facade is not removed in this release. A subsequent breaking
release may remove it only when:

1. `go list` and CI show no production dependency on the legacy facade;
2. the public migration guide has been published for one compatibility
   release;
3. external provider fixtures compile against only `pkg/domain`/`pkg/spi`;
4. HTTP, MCP, CLI, persistence, and provider integration tests pass using
   the public contracts;
5. the removal is called out as a major-version change.
