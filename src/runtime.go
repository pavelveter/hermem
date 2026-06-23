package main

import "database/sql"

// Runtime bundles per-process dependencies wired at startup. Holding
// these once on a single struct eliminates the previous package-level
// mutables: activeSchema (mutable process-wide schema) and iniRef
// (mutable INI parser state). After Runtime, per-request access flows
// through explicit receiver fields rather than implicit shared state,
// which unblocks multi-tenant and library use in the future.
//
// Construction pattern:
//   - main.go (production): cfg → NewRuntime(cfg) once; pass *Runtime
//     into StoreEntity / ProcessDialog / handlers.
//   - tests (helpers_test.go): memDB(t) factory stays, because most
//     unit tests do not need the full Runtime surface — they call
//     DB / VI / schema methods directly.
type Runtime struct {
	DB        *sql.DB
	VI        VectorIndex
	Embedder  Embedder
	Extractor LLMExtractor
	Config    *Config
}
