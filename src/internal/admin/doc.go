// Package admin provides offline database diagnostics and maintenance
// operations for Hermem. It is used exclusively through the CLI —
// the HTTP server never imports this package.
//
// The package exposes four capabilities:
//   - Stats:  snapshot of entity/edge counts, embedding coverage, DB size
//   - Integrity: three checks (missing embeddings, dangling edges,
//     archived entities referenced by non-archived ones)
//   - Vacuum:  SQLite VACUUM with progress callback
//   - RebuildIndex: selective vector-index rebuild with DryRun support
//
// Each capability is a self-contained struct (StatsCollector,
// IntegrityChecker, VacuumRunner, RebuildIndex) with its own set of
// tests. The structs accept *sql.DB plus optional interfaces
// (VectorIndex, Embedder) to support both production and test use.
package admin
