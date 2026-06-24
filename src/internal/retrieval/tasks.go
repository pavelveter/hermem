package retrieval

import (
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// GetExecutableTasks returns pending tasks with all blockers completed.
// If goalID is set, walks the goal's dependency subtree; otherwise scans all stateful categories.
func GetExecutableTasks(db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Entity, error) {
	if !schema.StatefulEnabled || len(schema.StatefulCategories) == 0 || len(schema.ValidStateOrder) == 0 {
		return []core.Entity{}, nil
	}
	if goalID != "" {
		return getExecutableForGoal(db, schema, goalID)
	}
	return getExecutableGlobal(db, schema)
}

func getExecutableForGoal(db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Entity, error) {
	catPH, catArgs := store.BoolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{goalID}, catArgs...)
	args = append(args, schema.RelationBlocking)
	args = append(args, catArgs...)
	args = append(args, schema.ValidStateOrder[0], schema.RelationBlocking, schema.StateUnblocking)
	query := fmt.Sprintf(`WITH RECURSIVE dep_tree AS (SELECT e.id, e.category, e.content, e.status, e.updated_at FROM entities e WHERE e.id = ? AND e.category IN (%s) AND e.archived = 0 UNION ALL SELECT e.id, e.category, e.content, e.status, e.updated_at FROM dep_tree dt JOIN edges ed ON ed.source_id = dt.id AND ed.relation_type = ? JOIN entities e ON e.id = ed.target_id AND e.category IN (%s) AND e.archived = 0) SELECT dt.id, dt.category, dt.content, dt.status, dt.updated_at, COALESCE(e.priority, 0) FROM dep_tree dt JOIN entities e ON e.id = dt.id WHERE dt.status = ? AND NOT EXISTS (SELECT 1 FROM edges ed2 WHERE ed2.source_id = dt.id AND ed2.relation_type = ? AND EXISTS (SELECT 1 FROM entities e3 WHERE e3.id = ed2.target_id AND e3.status != ?)) ORDER BY COALESCE(e.priority, 0) DESC, dt.id`, catPH, catPH)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("executable for goal: %w", err)
	}
	defer rows.Close()
	return store.ScanTaskEntities(rows)
}

func getExecutableGlobal(db *sql.DB, schema core.SchemaConfig) ([]core.Entity, error) {
	catPH, catArgs := store.BoolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{}, catArgs...)
	args = append(args, schema.ValidStateOrder[0], schema.RelationBlocking, schema.StateUnblocking)
	query := fmt.Sprintf(`SELECT e.id, e.category, e.content, e.status, e.updated_at, COALESCE(e.priority, 0) FROM entities e WHERE e.category IN (%s) AND e.status = ? AND e.archived = 0 AND NOT EXISTS (SELECT 1 FROM edges ed WHERE ed.source_id = e.id AND ed.relation_type = ? AND EXISTS (SELECT 1 FROM entities e2 WHERE e2.id = ed.target_id AND e2.status != ?)) ORDER BY COALESCE(e.priority, 0) DESC, e.id`, catPH)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("executable: %w", err)
	}
	defer rows.Close()
	return store.ScanTaskEntities(rows)
}
