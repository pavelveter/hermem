// Package graph hosts the transport-agnostic graph analytics domain
// service — connected components, community detection, and integrity
// verification.
package graph

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/graph/community"
	"github.com/pavelveter/hermem/src/internal/store"
)

// Service is the transport-agnostic graph analytics domain service.
//
// One dep at construction — db — matches the contradiction
// precedent. Schema + dim are passed per call so a SIGHUP reload that
// swaps cfg.Schema / cfg.VectorDim does not require reconstructing the
// service, and so the algorithm SQL picks up the active schema without
// holding state.
//
// No Refs/Metrics coupling — the HTTP shell increments counters on its
// own; the domain service is pure.
type Service struct {
	db *sql.DB
}

// New constructs a Service. db is required.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// Components returns all connected components of the entity graph with
// size ≥ minSize. minSize ≤ 0 means "no filter". Returns
// `[]ConnectedComponent{}` (not nil) on empty result so envelope
// serialization emits `[]` not `null`.
func (s *Service) Components(_ context.Context, minSize int) ([]core.ConnectedComponent, error) {
	return store.FindConnectedComponents(s.db, minSize)
}

// Communities runs Louvain-style community detection for up to maxIter
// iterations and returns the unfiltered list plus the global
// modularity. minSize filtering is left to the caller (HTTP shell or
// CLI) so the envelope can report total vs filtered counts
// side-by-side.
//
// Returns `[]Community{}` (not nil) on empty result.
func (s *Service) Communities(ctx context.Context, maxIter int) ([]core.Community, float64, error) {
	g, err := community.LoadGraph(ctx, s.db)
	if err != nil {
		return nil, 0, fmt.Errorf("communities: %w", err)
	}
	if g == nil {
		return make([]core.Community, 0), 0, nil
	}
	comms, globalQ := community.DetectCommunities(g, maxIter)
	if comms == nil {
		comms = make([]core.Community, 0)
	}
	return comms, globalQ, nil
}

// Verify runs a read-only integrity sweep over the graph and reports
// any orphan edges or embedding-dimension drift. dim is the current
// vector dimensionality — any entity whose BLOB length does not match
// dim*4 bytes is flagged. Returns a VerifyReport whose Pass() method
// controls CLI exit-1 semantics.
func (s *Service) Verify(ctx context.Context, schema core.SchemaConfig, vectorDim int) (core.VerifyReport, error) {
	var report core.VerifyReport

	orphanEdges, err := store.VerifyOrphanEdges(ctx, s.db)
	if err != nil {
		return report, err
	}
	for _, e := range orphanEdges {
		report.Issues = append(report.Issues, fmt.Sprintf("orphan edge: %s -[%s]-> %s", e.Source, e.Relation, e.Target))
	}

	dimMismatches, err := store.VerifyDimensionMismatches(ctx, s.db, vectorDim)
	if err != nil {
		return report, err
	}
	for _, m := range dimMismatches {
		report.Issues = append(report.Issues, fmt.Sprintf("dimension mismatch: %s has %d bytes (want %d)", m.ID, m.Bytes, vectorDim*4))
	}

	return report, nil
}
