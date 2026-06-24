// Package graph hosts the transport-agnostic graph analytics domain
// service — connected components, community detection, and integrity
// verification. PHASE 3.1 extracts these out of:
//   - src/internal/server/admin_service.go (god-object: /connected-
//     components + /communities HTTP routes)
//   - src/internal/cli/graph/{components,communities,verify}.go
//     (CLI handlers hitting store.* / algo.* directly)
//
// Mirrors the PHASE 2.x shape: flat pkg + transport-agnostic Service
// with per-call args for runtime-varying config (schema, dim) so the
// service stays free of long-lived state coupling.
package graph

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/algo"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// Service is the transport-agnostic graph analytics domain service.
//
// One dep at construction — db — matches the PHASE 2.3 contradiction
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

// NewService constructs a Service. db is required.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// Components returns all connected components of the entity graph with
// size ≥ minSize. minSize ≤ 0 means "no filter". Returns
// `[]ConnectedComponent{}` (not nil) on empty result so envelope
// serialization emits `[]` not `null`.
func (s *Service) Components(_ context.Context, minSize int) ([]core.ConnectedComponent, error) {
	comps, err := store.FindConnectedComponents(s.db, minSize)
	if err != nil {
		return nil, fmt.Errorf("components: %w", err)
	}
	if comps == nil {
		comps = []core.ConnectedComponent{}
	}
	return comps, nil
}

// Communities runs Louvain-style community detection for up to maxIter
// iterations and returns the unfiltered list plus the global
// modularity. minSize filtering is left to the caller (HTTP shell or
// CLI) so the envelope can report total vs filtered counts
// side-by-side.
//
// Returns `[]Community{}` (not nil) on empty result.
func (s *Service) Communities(_ context.Context, maxIter int) ([]core.Community, float64, error) {
	comms, globalQ, err := store.DetectCommunities(s.db, maxIter)
	if err != nil {
		return nil, 0, fmt.Errorf("communities: %w", err)
	}
	if comms == nil {
		comms = []core.Community{}
	}
	return comms, globalQ, nil
}

// Verify runs a read-only integrity sweep over the graph and reports
// any orphan edges or embedding-dimension drift. dim is the current
// vector dimensionality — any entity whose BLOB length does not match
// dim*4 bytes is flagged. Returns a VerifyReport whose Pass() method
// controls CLI exit-1 semantics.
func (s *Service) Verify(_ context.Context, schema core.SchemaConfig, dim int) (core.VerifyReport, error) {
	report, err := algo.VerifyGraph(s.db, schema, dim)
	if err != nil {
		return core.VerifyReport{}, fmt.Errorf("verify: %w", err)
	}
	return report, nil
}
