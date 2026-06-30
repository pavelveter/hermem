package evolution

import (
	"context"
	"database/sql"
	"fmt"
)

// MaxChainDepth caps belief revision chain traversal to prevent
// infinite loops from circular parent_chain_id references.
// See ADR-022 for rationale.
const MaxChainDepth = 32

// RevisionNode is one step in a belief revision chain.
type RevisionNode struct {
	ID         int64
	Confidence float64
	Status     string
	Content    string
	Depth      int
}

// TraceRevisions walks the revision chain (parent_chain_id) of a belief
// from the given ID backward to the root, then returns an ordered list
// from the oldest ancestor to the latest (root first). Depth is bounded
// by MaxChainDepth.
//
// Uses a single recursive CTE (N+1-safe) to follow parent_chain_id
// backward, then reverses the result in Go so the caller sees a
// chronological (oldest-first) sequence.
func TraceRevisions(ctx context.Context, db *sql.DB, beliefID int64) ([]RevisionNode, error) {
	if beliefID <= 0 {
		return nil, fmt.Errorf("evolution: invalid belief ID %d", beliefID)
	}

	q := `
	WITH RECURSIVE chain AS (
		SELECT id, confidence, status, content, parent_chain_id, 0 AS depth
		FROM beliefs WHERE id = ?
		UNION ALL
		SELECT b.id, b.confidence, b.status, b.content, b.parent_chain_id, c.depth + 1
		FROM beliefs b
		JOIN chain c ON b.id = c.parent_chain_id
		WHERE c.depth < ?
	)
	SELECT id, confidence, status, content, depth
	FROM chain
	ORDER BY depth DESC, id DESC
	`
	rows, err := db.QueryContext(ctx, q, beliefID, MaxChainDepth)
	if err != nil {
		return nil, fmt.Errorf("evolution: trace revisions: %w", err)
	}
	defer rows.Close()

	var out []RevisionNode
	for rows.Next() {
		var n RevisionNode
		if err := rows.Scan(&n.ID, &n.Confidence, &n.Status, &n.Content, &n.Depth); err != nil {
			return nil, fmt.Errorf("evolution: scan revision: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evolution: rows: %w", err)
	}
	return out, nil
}
