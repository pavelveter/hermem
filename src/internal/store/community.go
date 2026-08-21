package store

import (
	"context"
	"database/sql"

	"github.com/pavelveter/hermem/pkg/domain"
	"github.com/pavelveter/hermem/src/internal/graph/community"
)

// DetectCommunities runs Louvain community detection on the graph.
// Deprecated: Use community.LoadGraph + community.DetectCommunities directly.
func DetectCommunities(ctx context.Context, db *sql.DB, maxIterations int) ([]domain.Community, float64, error) {
	g, err := community.LoadGraph(ctx, db)
	if err != nil {
		return nil, 0, err
	}
	if g == nil {
		return nil, 0, nil
	}
	comms, globalQ := community.DetectCommunities(g, maxIterations)
	return comms, globalQ, nil
}
