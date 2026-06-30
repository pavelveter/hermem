# ADR-013: Migration Ownership

## Status
Accepted

## Context
`store/` owned all migration logic (SQL execution, checksums, embedded FS). `internal/migration/` was a thin facade. Callers (`cli/db`, `server/migration`) imported `store` types directly, creating tight coupling between CLI/HTTP shells and the SQL layer.

## Decision
1. **`core.Migrator` interface** in `core/types.go` defines the minimal migration surface: `Run`, `DryRun`, `Status`, `Verify`, `Rollback`. Lightweight `core.MigrationStatus` and `core.MigrationMismatch` types avoid a `core → store` dependency.
2. **`migration.Service`** implements `core.Migrator` with `store → core` adapters. Added `SchemaFingerprint(ctx, schema)` as a concrete method (write op, not on the interface).
3. **Callers** (`cli/db`, `server/migration`) now depend on `migration.Service` return types (`core.*`), not `store.*` directly.
4. **`cli/serve.go`** keeps calling `store.StoreSchemaFingerprint` directly — it's a bootstrapping mutation outside the request lifecycle, deliberately not on the facade.

## Consequences
- `core` package stays dependency-free (no `store` import)
- Migration facade is testable via `core.Migrator` mock
- `store` package remains the single source of truth for SQL execution and checksums
