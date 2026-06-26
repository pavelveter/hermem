package store

import (
	"context"
	"database/sql"
	"fmt"
)

// VerifyOrphanEdges returns edges whose source or target entity is missing.
func VerifyOrphanEdges(ctx context.Context, db *sql.DB) ([]OrphanEdge, error) {
	rows, err := db.QueryContext(ctx, `SELECT ed.source_id, ed.target_id, ed.relation_type FROM edges ed LEFT JOIN entities e1 ON ed.source_id = e1.id LEFT JOIN entities e2 ON ed.target_id = e2.id WHERE e1.id IS NULL OR e2.id IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("verify orphan edges: %w", err)
	}
	defer rows.Close()
	var edges []OrphanEdge
	for rows.Next() {
		var e OrphanEdge
		rows.Scan(&e.Source, &e.Target, &e.Relation) //nolint:errcheck // iteration error surfaces via rows.Err() check below
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("verify orphan edges: %w", err)
	}
	return edges, nil
}

// OrphanEdge is an edge with a missing source or target entity.
type OrphanEdge struct {
	Source   string
	Target   string
	Relation string
}

// VerifyDimensionMismatches returns entities whose embedding BLOB length
// doesn't match the expected dimension.
func VerifyDimensionMismatches(ctx context.Context, db *sql.DB, vectorDim int) ([]DimensionMismatch, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, length(embedding) FROM entities WHERE archived = 0 AND embedding IS NOT NULL AND length(embedding) != ?`, vectorDim*4)
	if err != nil {
		return nil, fmt.Errorf("verify dim: %w", err)
	}
	defer rows.Close()
	var mismatches []DimensionMismatch
	for rows.Next() {
		var m DimensionMismatch
		rows.Scan(&m.ID, &m.Bytes) //nolint:errcheck // iteration error surfaces via rows.Err() check below
		mismatches = append(mismatches, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("verify dim: %w", err)
	}
	return mismatches, nil
}

// DimensionMismatch is an entity whose embedding BLOB length doesn't
// match the expected dimension.
type DimensionMismatch struct {
	ID    string
	Bytes int
}
