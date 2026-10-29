package retrieval

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
func expandGraph(db *sql.DB, seedIDs []string, opts RetrieveContextOptions, effDepth int) ([]scannedNode, error) {
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
	args = append(args, effDepth, effDepth)

	// The recursive step is written as two UNION ALL branches (outgoing edges,
	// then incoming edges) instead of one join with
	// `(ed.source_id = gw.id OR ed.target_id = gw.id)` plus a CASE-expression
	// entity join. SQLite cannot turn the CASE form into a primary-key lookup:
	// it builds an automatic partial index on entities(archived) and scans every
	// live entity for each edge row, which made the walk quadratic in graph size.
	// The split form searches edges by source_id/target_id and entities by id.
	//
	// The embedding BLOB is only selected when the caller needs vectors for
	// scoring: it is the bulk of each row's payload and most callers walk the
	// graph without a query embedding.
	embCol := "NULL AS embedding"
	if len(opts.QueryEmbedding) > 0 {
		embCol = "e.embedding"
	}
	const walkBranch = `
			SELECT e.id, e.category, e.content, e.updated_at, %s, e.degree, gw.depth + 1, gw.path_weight + COALESCE(ed.weight, 1.0), gw.id, ed.relation_type, gw.visited || e.id || char(31)
			FROM graph_walk gw JOIN edges ed ON %s JOIN entities e ON e.id = %s
			WHERE gw.depth < ? AND instr(gw.visited, char(31) || e.id || char(31)) = 0 AND e.archived = 0`
	query := `
		WITH RECURSIVE graph_walk AS (
			SELECT e.id, e.category, e.content, e.updated_at, ` + embCol + `, e.degree, 0 as depth, 0.0 as path_weight, '' as parent_id, '' as relation_type, char(31) || e.id || char(31) as visited
			FROM entities e WHERE e.id IN (` + phs + `) AND e.archived = 0` + timeFilter + `
			UNION ALL` +
		fmt.Sprintf(walkBranch, embCol, "ed.source_id = gw.id", "ed.target_id") + `
			UNION ALL` +
		fmt.Sprintf(walkBranch, embCol, "ed.target_id = gw.id", "ed.source_id") + `
		)
		SELECT id, category, content, unixepoch(updated_at), embedding, degree, depth, path_weight, parent_id, relation_type FROM graph_walk ORDER BY depth ASC, category ASC
	`

	rows, err := queryCached(context.Background(), db, query, args...)
	if err != nil {
		return nil, fmt.Errorf("graph walk: %w", err)
	}
	defer rows.Close()

	// Pre-size for the common case (tens of nodes) so the walk does not
	// repeatedly reallocate and copy the growing result slice.
	capHint := 32
	if opts.MaxRetrievedNodes > 0 && opts.MaxRetrievedNodes < capHint {
		capHint = opts.MaxRetrievedNodes
	}
	nodes := make([]scannedNode, 0, capHint)
	seenIDs := make(map[string]bool, len(seedIDs)*2)
	for rows.Next() {
		var node GraphNode
		var updatedAtUnix int64
		var embBlob []byte
		if err := rows.Scan(&node.Entity.ID, &node.Entity.Category, &node.Entity.Content, &updatedAtUnix, &embBlob, &node.Entity.Degree, &node.Depth, &node.PathWeight, &node.ParentID, &node.RelationType); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if updatedAtUnix > 0 {
			t := time.Unix(updatedAtUnix, 0).UTC()
			node.Entity.UpdatedAt = &t
		}
		if seenIDs[node.Entity.ID] {
			continue
		}
		seenIDs[node.Entity.ID] = true
		if opts.MaxRetrievedNodes > 0 && len(seenIDs) > opts.MaxRetrievedNodes {
			break
		}
		var vec []float32
		if len(opts.QueryEmbedding) > 0 {
			vec, _ = store.DecodeVector(embBlob, len(opts.QueryEmbedding)) //nolint:errcheck // load-bearing: empty vector falls back to sim=0; caller treats missing dimensions as no-match
		}
		nodes = append(nodes, scannedNode{node: node, vec: vec})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("graph walk rows: %w", err)
	}
	return nodes, nil
}

// scannedNode is the internal handoff between expandGraph and
// scoreAndRank — the GraphNode plus its decoded embedding. Kept
// package-private; it is not part of any public contract. Lives in
// expand.go because it is the expand stage's output type.
type scannedNode struct {
	node GraphNode
	vec  []float32
}
