// Package view builds read-only snapshots of the knowledge graph for
// `hermem view` — a bounded node/edge projection an operator can look at
// in a browser.
//
// It deliberately owns no HTTP surface: the CLI command serves the
// embedded page and JSON API so the snapshot stays testable without a
// listener.
package view

import (
	"context"
	"database/sql"
	"fmt"
)

// NodeCap bounds how many entities a snapshot carries.
//
// ponytail: the page runs an O(n²) force simulation per frame, so 500 is
// the ceiling that stays interactive on a laptop. When that becomes a
// limit, raise the cap and swap the simulation for a spatial index (or
// Barnes-Hut) instead of lifting this constant blindly.
const NodeCap = 500

// Node is one entity in the snapshot.
type Node struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Content  string `json:"content"`
	Degree   int    `json:"degree"`
	Status   string `json:"status,omitempty"`
}

// Edge is one relation between two snapshot nodes.
type Edge struct {
	SourceID     string  `json:"source_id"`
	TargetID     string  `json:"target_id"`
	RelationType string  `json:"relation_type"`
	Weight       float32 `json:"weight,omitempty"`
}

// Graph is the snapshot payload served to the page.
type Graph struct {
	Nodes      []Node `json:"nodes"`
	Edges      []Edge `json:"edges"`
	TotalNodes int64  `json:"total_nodes"`
	TotalEdges int64  `json:"total_edges"`
	Truncated  bool   `json:"truncated"`
}

// Snapshot reads up to limit entities (highest degree first) plus the
// edges whose both endpoints survive the cap.
//
// limit <= 0 or limit > NodeCap falls back to NodeCap; the cap is a page
// performance ceiling, not a caller preference.
func Snapshot(ctx context.Context, db *sql.DB, limit int) (Graph, error) {
	if limit <= 0 || limit > NodeCap {
		limit = NodeCap
	}
	g := Graph{Nodes: []Node{}, Edges: []Edge{}}

	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities WHERE archived = 0`).Scan(&g.TotalNodes); err != nil {
		return g, fmt.Errorf("view: count entities: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges`).Scan(&g.TotalEdges); err != nil {
		return g, fmt.Errorf("view: count edges: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, category, content, COALESCE(status, ''), COALESCE(degree, 0)
		FROM entities
		WHERE archived = 0
		ORDER BY COALESCE(degree, 0) DESC, id
		LIMIT ?`, limit)
	if err != nil {
		return g, fmt.Errorf("view: entities: %w", err)
	}
	defer rows.Close()

	seen := make(map[string]bool, limit)
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.Category, &n.Content, &n.Status, &n.Degree); err != nil {
			return g, fmt.Errorf("view: scan entity: %w", err)
		}
		g.Nodes = append(g.Nodes, n)
		seen[n.ID] = true
	}
	if err := rows.Err(); err != nil {
		return g, fmt.Errorf("view: entity rows: %w", err)
	}

	edgeRows, err := db.QueryContext(ctx, `
		SELECT source_id, target_id, relation_type, COALESCE(weight, 1.0)
		FROM edges
		ORDER BY source_id, target_id, relation_type`)
	if err != nil {
		return g, fmt.Errorf("view: edges: %w", err)
	}
	defer edgeRows.Close()

	for edgeRows.Next() {
		var e Edge
		if err := edgeRows.Scan(&e.SourceID, &e.TargetID, &e.RelationType, &e.Weight); err != nil {
			return g, fmt.Errorf("view: scan edge: %w", err)
		}
		// Endpoints outside the node page would render as dangling links.
		if !seen[e.SourceID] || !seen[e.TargetID] {
			continue
		}
		g.Edges = append(g.Edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		return g, fmt.Errorf("view: edge rows: %w", err)
	}

	g.Truncated = g.TotalNodes > int64(len(g.Nodes))
	return g, nil
}
