package store

import (
	"context"
	"database/sql"
	"errors"
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

// ClaimNextTask atomically claims the highest-priority pending task for
// processing. It uses UPDATE...RETURNING so two concurrent callers
// cannot claim the same row: SQLite's single-writer model serialises the
// UPDATEs, and the inner SELECT's `e.status = ?` (Pending) re-evaluates
// against the freshly-flipped value when the second caller reaches the
// row. Returns nil (not an error) when no tasks are available.
//
// Portability note: the race-freedom above relies on SQLite-internal
// write serialization. If the storage engine ever switches to a
// multi-writer RDBMS (Postgres, MySQL), wrap the UPDATE in an
// explicit `SELECT ... FOR UPDATE SKIP LOCKED` so the read+write pair
// in the inner subquery cannot race against concurrent claims.
//
// Context-cancel leak risk: mattn/go-sqlite3 commits the UPDATE the
// instant the statement crosses the wire; if ctx is cancelled after the
// commit but before QueryRowContext returns the row, the task is left
// in the processing state with no caller to claim it. A future
// iteration should either wrap the UPDATE in an explicit tx + ROLLBACK
// on ctx.Err(), or gate the claim on a follow-up ownership heartbeat.
func ClaimNextTask(ctx context.Context, db *sql.DB, schema core.SchemaConfig, goalID string) (*core.Task, error) {
	if !schema.StatefulEnabled || len(schema.StatefulCategories) == 0 || len(schema.ValidStateOrder) == 0 {
		return nil, nil
	}

	catPH, catArgs := BoolMapInClause(schema.StatefulCategories)
	processingStatus := schema.ValidStateOrder[0]
	if len(schema.ValidStateOrder) > 1 {
		processingStatus = schema.ValidStateOrder[1]
	}

	var query string
	var args []interface{}

	if goalID != "" {
		// Atomic claim within a goal subtree
		//
		// SQL has 6 fixed placeholders + 3 * N_cat cat placeholders (= 9
		// for the 1-category statefulSchema; 12 for a hypothetical 2-cat
		// schema). Args below mirror that ordering exactly: SET status,
		// cat #1, status guard, goalID, cat #2, blocker relation, cat #3,
		// blocker relation (NOT EXISTS), state-unblocking guard. Prior to
		// the §1 audit-closure fix, catArgs was discarded via `_` and the
		// outer append populated only the fixed positions — mattn/go-sqlite3
		// then refused to bind with "not enough args to execute query:
		// want 9 got 6".
		query = fmt.Sprintf(`UPDATE entities
			SET status = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id IN (
				SELECT e.id FROM entities e
				WHERE e.category IN (%s) AND e.status = ? AND e.archived = 0
				AND e.id IN (
					SELECT dt.id FROM (
						SELECT e2.id FROM entities e2 WHERE e2.id = ? AND e2.category IN (%s) AND e2.archived = 0
						UNION ALL
						SELECT e3.id FROM entities e3
						JOIN edges ed ON ed.source_id = e3.id AND ed.relation_type = ?
						JOIN entities e4 ON e4.id = ed.target_id AND e4.category IN (%s) AND e4.archived = 0
					) dt
				)
				AND NOT EXISTS (
					SELECT 1 FROM edges ed2 WHERE ed2.target_id = e.id AND ed2.relation_type = ?
					AND EXISTS (SELECT 1 FROM entities e5 WHERE e5.id = ed2.source_id AND e5.status != ?)
				)
				ORDER BY COALESCE(e.priority, 0) DESC, e.id
				LIMIT 1
			)
			RETURNING id, category, content, status, COALESCE(priority, 0)`, catPH, catPH, catPH)
		args = []interface{}{processingStatus}
		args = append(args, catArgs...) // cat #1: outer WHERE category IN
		args = append(args, schema.ValidStateOrder[0], goalID)
		args = append(args, catArgs...) // cat #2: dt subquery seed branch
		args = append(args, schema.RelationBlocking)
		args = append(args, catArgs...) // cat #3: dt subquery descendants branch
		args = append(args, schema.RelationBlocking, schema.StateUnblocking)
	} else {
		// Global atomic claim
		//
		// SQL has 4 fixed placeholders + N_cat cat placeholders (= 5 for
		// the 1-category statefulSchema; 6 for a 2-cat schema). Args below
		// mirror that ordering: SET status, cat, status guard, blocker
		// relation, state-unblocking guard. Prior to the §1 audit-closure
		// fix, catArgs was discarded via `_` and the outer append built
		// only the 4 fixed positions — the driver refused to bind with
		// "not enough args to execute query: want 5 got 4".
		query = fmt.Sprintf(`UPDATE entities
			SET status = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id IN (
				SELECT e.id FROM entities e
				WHERE e.category IN (%s) AND e.status = ? AND e.archived = 0
				AND NOT EXISTS (
					SELECT 1 FROM edges ed WHERE ed.target_id = e.id AND ed.relation_type = ?
					AND EXISTS (SELECT 1 FROM entities e2 WHERE e2.id = ed.source_id AND e2.status != ?)
				)
				ORDER BY COALESCE(e.priority, 0) DESC, e.id
				LIMIT 1
			)
			RETURNING id, category, content, status, COALESCE(priority, 0)`, catPH)
		args = []interface{}{processingStatus}
		args = append(args, catArgs...) // cat: outer WHERE category IN
		args = append(args, schema.ValidStateOrder[0])
		args = append(args, schema.RelationBlocking)
		args = append(args, schema.StateUnblocking)
	}

	var task core.Task
	err := db.QueryRowContext(ctx, query, args...).Scan(
		&task.ID, &task.Category, &task.Content, &task.Status, &task.Priority,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // no tasks available
		}
		return nil, fmt.Errorf("claim next task: %w", err)
	}
	return &task, nil
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
