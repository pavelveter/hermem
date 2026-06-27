package store

import (
	"context"
	"database/sql"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/graph/community"
)

// DetectCommunities runs Louvain community detection on the graph.
// Deprecated: Use community.LoadGraph + community.DetectCommunities directly.
func DetectCommunities(db *sql.DB, maxIterations int) ([]core.Community, float64, error) {
	g, err := community.LoadGraph(context.Background(), db)
	if err != nil {
		return nil, 0, err
	}
	if g == nil {
		return nil, 0, nil
	}
	comms, globalQ := community.DetectCommunities(g, maxIterations)
	return comms, globalQ, nil
}
