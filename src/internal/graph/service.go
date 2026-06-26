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
func New(db *sql.DB) *Service {
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
	return core.NormalizeSlice(comps), nil
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
	return core.NormalizeSlice(comms), globalQ, nil
}

// Verify runs a read-only integrity sweep over the graph and reports
// any orphan edges or embedding-dimension drift. dim is the current
// vector dimensionality — any entity whose BLOB length does not match
// dim*4 bytes is flagged. Returns a VerifyReport whose Pass() method
// controls CLI exit-1 semantics.
//
// PHASE 3.9: inlined from algo.VerifyGraph (now deleted from algo/verify.go).
func (s *Service) Verify(_ context.Context, schema core.SchemaConfig, vectorDim int) (core.VerifyReport, error) {
	var report core.VerifyReport
	rows, err := s.db.Query(`SELECT ed.source_id, ed.target_id, ed.relation_type FROM edges ed LEFT JOIN entities e1 ON ed.source_id = e1.id LEFT JOIN entities e2 ON ed.target_id = e2.id WHERE e1.id IS NULL OR e2.id IS NULL`)
	if err != nil {
		return report, fmt.Errorf("verify orphan edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var src, dst, rel string
		rows.Scan(&src, &dst, &rel) //nolint:errcheck // iteration error surfaces via rows.Err() check below
		report.Issues = append(report.Issues, fmt.Sprintf("orphan edge: %s -[%s]-> %s", src, rel, dst))
	}
	if err := rows.Err(); err != nil {
		return report, fmt.Errorf("verify orphan edges: %w", err)
	}
	embRows, err := s.db.Query(`SELECT id, length(embedding) FROM entities WHERE archived = 0 AND embedding IS NOT NULL AND length(embedding) != ?`, vectorDim*4)
	if err != nil {
		return report, fmt.Errorf("verify dim: %w", err)
	}
	defer embRows.Close() //nolint:errcheck // standard Go idiom — keep-alive pool drains on next reuse
	for embRows.Next() {
		var id string
		var l int
		embRows.Scan(&id, &l) //nolint:errcheck // iteration error surfaces via embRows.Err() check below
		report.Issues = append(report.Issues, fmt.Sprintf("dimension mismatch: %s has %d bytes (want %d)", id, l, vectorDim*4))
	}
	if err := embRows.Err(); err != nil {
		return report, fmt.Errorf("verify dim: %w", err)
	}
	return report, nil
}
