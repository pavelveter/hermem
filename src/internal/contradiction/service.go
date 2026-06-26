// Package contradiction hosts the read-side domain logic for inspecting
// pre-existing contradiction edges in the graph.
//
// PHASE 2.3 introduction — until this commit HandleContradictions lived
// inside src/internal/server/retrieval/retrieval_service.go and reached
// the underlying *sql.DB through a temporary accessor on retrieval.Service.
// PHASE 2.3 moves the route into its own transport shell
// (src/internal/server/contradiction) and removes the temporary
// accessor. The Service here is a thin wrapper around
// store.GetContradictions — its only job is to give the HTTP shell and
// the CLI a uniform ctx-based API without each handler reaching
// directly into the store pkg.
//
// Ingest-time contradiction detection (extractor.IsIngestionContradiction
// in src/internal/ingestion/dialog.go) is a different concern entirely:
// a text-based dedup heuristic used during ingest, not a read-side
// query. It stays in the ingestion pkg and is NOT moved here.
package contradiction

import (
	"context"
	"database/sql"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// Service is the transport-agnostic read-side API for contradiction edges.
//
// Holds only *sql.DB — reading contradictions is a pure SQL join that
// has no need for the vector index, the embedder, or the schema state.
// Adding other read-only deps would be unnecessary surface for callers
// and wouldn't be exercised by tests. Minimal interface, easy to
// evolve if/when we add ctx-aware variants (time-windowed queries,
// confidence-threshold filters, etc.).
type Service struct {
	db *sql.DB
}

// NewService constructs a Service. db must be non-nil.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// List returns all contradicts edges, optionally filtered by entityID.
//
// Empty entityID means "all contradiction pairs in the graph"; non-empty
// restricts to pairs where entityID is either the source or the target.
// The underlying store.GetContradictions does the SQL filter; this
// method is a one-line pass-through plus nil→empty normalization so the
// JSON envelope always emits `[]` instead of `null` on empty result.
//
// ctx is reserved for future ctx-aware variants (cancellation, timeouts
// against large graphs); the current SQL is fast enough that explicit
// threading isn't required. Passing-through ctx here is cheap and keeps
// the signature parity with retrieval.Service.
func (s *Service) List(_ context.Context, entityID string) ([]core.ContradictionPair, error) {
	pairs, err := store.GetContradictions(s.db, entityID)
	if err != nil {
		return nil, err
	}
	return core.NormalizeSlice(pairs), nil
}
