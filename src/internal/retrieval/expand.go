package retrieval

import (
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// This file owns the retrieval pipeline's first stage — graph expansion
// via the recursive CTE. Pulled out of walk.go so the stage boundary is
// visible at the file level: a reader scanning retrieval/ can see the
// pipeline as expand.go → scoring.go → walk.go → bucketize / rerank
// helpers, with each file owning exactly one concern.
//
// expandGraph — stage 1 of the retrieval pipeline. Executes the
// recursive CTE that walks the graph from seed IDs up to effDepth,
// returning one scannedNode per distinct entity reached (depth-0 seeds
// included). Honors MaxRetrievedNodes as a soft cap. Applies
// TimeFrom / TimeTo filters when set on opts.
//
// Stage output is unscored — scoreAndRank owns the scoring pass so
// each row is decoded exactly once. The decoded embedding vector is
// returned alongside the node so stage 2 can feed it into the scorer
// without re-querying the row.
func expandGraph(db *sql.DB, seedIDs []string, opts core.RetrieveContextOptions, effDepth int) ([]scannedNode, error) {
	phs, args := store.InClauseArgs(seedIDs)
	var timeFilter string
	if !opts.TimeFrom.IsZero() {
		timeFilter += " AND e.created_at >= ?"
		args = append(args, opts.TimeFrom)
	}
	if !opts.TimeTo.IsZero() {
		timeFilter += " AND e.created_at <= ?"
		args = append(args, opts.TimeTo)
	}
	args = append(args, effDepth)

	query := fmt.Sprintf(`
		WITH RECURSIVE graph_walk AS (
			SELECT e.id, e.category, e.content, e.updated_at, e.embedding, e.degree, 0 as depth, 0.0 as path_weight, '' as parent_id, '' as relation_type, char(31) || e.id || char(31) as visited
			FROM entities e WHERE e.id IN (%s) AND e.archived = 0`+timeFilter+`
			UNION ALL
			SELECT e.id, e.category, e.content, e.updated_at, e.embedding, e.degree, gw.depth + 1, gw.path_weight + COALESCE(ed.weight, 1.0), gw.id, ed.relation_type, gw.visited || e.id || char(31)
			FROM graph_walk gw JOIN edges ed ON (ed.source_id = gw.id OR ed.target_id = gw.id)
			JOIN entities e ON (CASE WHEN ed.source_id = gw.id THEN ed.target_id = e.id ELSE ed.source_id = e.id END)
			WHERE gw.depth < ? AND instr(gw.visited, char(31) || e.id || char(31)) = 0 AND e.archived = 0`+timeFilter+`
		)
		SELECT DISTINCT id, category, content, updated_at, embedding, degree, depth, path_weight, parent_id, relation_type FROM graph_walk ORDER BY depth ASC, category ASC
	`, phs)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("graph walk: %w", err)
	}
	defer rows.Close()

	var nodes []scannedNode
	seenIDs := make(map[string]bool)
	for rows.Next() {
		var node core.GraphNode
		var embBlob []byte
		if err := rows.Scan(&node.Entity.ID, &node.Entity.Category, &node.Entity.Content, &node.Entity.UpdatedAt, &embBlob, &node.Entity.Degree, &node.Depth, &node.PathWeight, &node.ParentID, &node.RelationType); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if seenIDs[node.Entity.ID] {
			continue
		}
		seenIDs[node.Entity.ID] = true
		if opts.MaxRetrievedNodes > 0 && len(seenIDs) > opts.MaxRetrievedNodes {
			break
		}
		vec, _ := store.DecodeVector(embBlob, len(opts.QueryEmbedding)) //nolint:errcheck // load-bearing: empty vector falls back to sim=0; caller treats missing dimensions as no-match
		nodes = append(nodes, scannedNode{node: node, vec: vec})
	}
	return nodes, nil
}

// scannedNode is the internal handoff between expandGraph and
// scoreAndRank — the GraphNode plus its decoded embedding. Kept
// package-private; it is not part of any public contract. Lives in
// expand.go because it is the expand stage's output type.
type scannedNode struct {
	node core.GraphNode
	vec  []float32
}
