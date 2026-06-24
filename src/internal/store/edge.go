package store

import (
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
)

// AddEdge inserts an edge between two existing entities.
func AddEdge(db *sql.DB, src, dst, rel string, weight float32) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM entities WHERE id IN (?, ?)", src, dst).Scan(&count); err != nil {
		return fmt.Errorf("failed to check entity existence: %w", err)
	}
	if count != 2 {
		return fmt.Errorf("both source and target entities must exist (found %d of 2)", count)
	}
	if weight == 0 {
		weight = 1.0
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, ?, ?)`, src, dst, rel, weight)
	if err != nil {
		return fmt.Errorf("failed to insert edge: %w", err)
	}
	return nil
}

// DeleteEdge removes a single edge row.
func DeleteEdge(db *sql.DB, src, dst, rel string) error {
	_, err := db.Exec("DELETE FROM edges WHERE source_id = ? AND target_id = ? AND relation_type = ?", src, dst, rel)
	if err != nil {
		return fmt.Errorf("delete edge: %w", err)
	}
	return nil
}

// QueryEdges runs a query and scans all rows into a core.Edge slice.
func QueryEdges(db *sql.DB, query string, args ...interface{}) ([]core.Edge, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Edge
	for rows.Next() {
		var ed core.Edge
		if err := rows.Scan(&ed.SourceID, &ed.TargetID, &ed.RelationType, &ed.Weight); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		out = append(out, ed)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edges: %w", err)
	}
	return out, nil
}
