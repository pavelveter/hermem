// Package edge owns the transport-agnostic relation-edge write API.
//
// PHASE 3.5 lifts AddEdge out of memory.Service into its own flat pkg
// following the PHASE 2.x + PHASE 3.1 + 3.2 + 3.3 + 3.4 precedent: flat
// pkg, stateless Service, per-call args for things that change
// request-time (schema), no HTTP / CLI coupling. The HTTP shell lives
// in src/internal/server/edge/.
//
// The auto-create code path (AddEdgeRequest.AutoCreate=true) uses
// vector.AddEdgeWithAutoCreate which threads the embedder + vector
// index through to AutoLinkEdges-equivalent behaviour. Both branches
// (auto_create true / false) resolve through SchemaConfig.AllowedRelations
// before touching the DB so a malformed request cannot bypass
// write-side guards.
package edge

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Service is the transport-agnostic edge domain API.
//
// Mirrors pre-PHASE-3.5 memory.Service field shape: db + vi + embedder.
// After PHASE 3.5 the memory domain keeps Store + StoreAndLink only;
// edge domain owns AddEdge exclusively. vi + embedder are unused on the
// non-auto-create path; they're held here so AddEdge can dispatch to
// vector.AddEdgeWithAutoCreate without constructor branching.
type Service struct {
	db       *sql.DB
	vi       core.VectorIndex
	embedder core.Embedder
}

// New constructs an edge Service. All three deps are required:
//   - db:    AddEdge + auto-create path write the relation row.
//   - vi:    auto-create path may need to delete/upsert in the in-mem index.
//   - embedder: auto-create path embeds newly-synthesised endpoint entities.
//   - On the non-auto-create path vi + embedder are unused but the
//     constructor still requires them at boot so caller-side wiring
//     fails fast at daemon startup if any dep is nil.
func New(db *sql.DB, vi core.VectorIndex, embedder core.Embedder) *Service {
	return &Service{db: db, vi: vi, embedder: embedder}
}

// AddEdge persists a relation edge, optionally auto-creating missing
// endpoint entities via vector.AddEdgeWithAutoCreate.
//
// Validation precedes dispatch: unknown relation_type → ErrInvalidSchema
// with Field="relation_type". Both branches (auto_create true/false)
// resolve through SchemaConfig.AllowedRelations before touching the DB
// so a malformed request cannot bypass write-side guards.
func (s *Service) AddEdge(ctx context.Context, req core.EdgeRequest, schema core.SchemaConfig) error {
	if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
		return fmt.Errorf("edge: source_id, target_id, relation_type required")
	}
	if !schema.AllowedRelations[req.RelationType] {
		return core.NewInvalidSchemaError("relation_type", req.RelationType)
	}
	if req.AutoCreate {
		if err := vector.AddEdgeWithAutoCreate(ctx, s.db, s.vi, s.embedder, req.SourceID, req.TargetID, req.RelationType); err != nil {
			return fmt.Errorf("edge auto-create: %w", err)
		}
		return nil
	}
	if err := store.AddEdge(ctx, s.db, req.SourceID, req.TargetID, req.RelationType, req.Weight); err != nil {
		return fmt.Errorf("edge: %w", err)
	}
	return nil
}
