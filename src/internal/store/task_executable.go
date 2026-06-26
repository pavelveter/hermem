package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
)

// GetExecutableTasks returns tasks that are pending with no unfinished
// blockers. goalID narrows the search to a subtree; empty means global.
func GetExecutableTasks(ctx context.Context, db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Task, error) {
	if !schema.StatefulEnabled || len(schema.StatefulCategories) == 0 || len(schema.ValidStateOrder) == 0 {
		return []core.Task{}, nil
	}
	if goalID != "" {
		return getExecutableForGoal(ctx, db, schema, goalID)
	}
	return getExecutableGlobal(ctx, db, schema)
}

func getExecutableForGoal(ctx context.Context, db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Task, error) {
	catPH, catArgs := BoolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{goalID}, catArgs...)
	args = append(args, schema.RelationBlocking)
	args = append(args, catArgs...)
	args = append(args, schema.ValidStateOrder[0], schema.RelationBlocking, schema.StateUnblocking)
	query := fmt.Sprintf(`WITH RECURSIVE dep_tree AS (SELECT e.id, e.category, e.content, e.status, e.updated_at FROM entities e WHERE e.id = ? AND e.category IN (%s) AND e.archived = 0 UNION ALL SELECT e.id, e.category, e.content, e.status, e.updated_at FROM dep_tree dt JOIN edges ed ON ed.source_id = dt.id AND ed.relation_type = ? JOIN entities e ON e.id = ed.target_id AND e.category IN (%s) AND e.archived = 0) SELECT dt.id, dt.category, dt.content, dt.status, dt.updated_at, COALESCE(e.priority, 0) FROM dep_tree dt JOIN entities e ON e.id = dt.id WHERE dt.status = ? AND NOT EXISTS (SELECT 1 FROM edges ed2 WHERE ed2.target_id = dt.id AND ed2.relation_type = ? AND EXISTS (SELECT 1 FROM entities e3 WHERE e3.id = ed2.source_id AND e3.status != ?)) ORDER BY COALESCE(e.priority, 0) DESC, dt.id`, catPH, catPH)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executable for goal: %w", err)
	}
	defer rows.Close()
	return ScanTaskEntities(rows)
}

func getExecutableGlobal(ctx context.Context, db *sql.DB, schema core.SchemaConfig) ([]core.Task, error) {
	catPH, catArgs := BoolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{}, catArgs...)
	args = append(args, schema.ValidStateOrder[0], schema.RelationBlocking, schema.StateUnblocking)
	query := fmt.Sprintf(`SELECT e.id, e.category, e.content, e.status, e.updated_at, COALESCE(e.priority, 0) FROM entities e WHERE e.category IN (%s) AND e.status = ? AND e.archived = 0 AND NOT EXISTS (SELECT 1 FROM edges ed WHERE ed.target_id = e.id AND ed.relation_type = ? AND EXISTS (SELECT 1 FROM entities e2 WHERE e2.id = ed.source_id AND e2.status != ?)) ORDER BY COALESCE(e.priority, 0) DESC, e.id`, catPH)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executable: %w", err)
	}
	defer rows.Close()
	return ScanTaskEntities(rows)
}
