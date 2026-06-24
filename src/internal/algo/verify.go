package algo

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/store"
)

// VerifyGraph runs read-only integrity checks (orphan edges, embedding dim drift).
func VerifyGraph(db *sql.DB, schema core.SchemaConfig, vectorDim int) (core.VerifyReport, error) {
	var report core.VerifyReport
	rows, err := db.Query(`SELECT ed.source_id, ed.target_id, ed.relation_type FROM edges ed LEFT JOIN entities e1 ON ed.source_id = e1.id LEFT JOIN entities e2 ON ed.target_id = e2.id WHERE e1.id IS NULL OR e2.id IS NULL`)
	if err != nil {
		return report, fmt.Errorf("verify orphan edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var src, dst, rel string
		rows.Scan(&src, &dst, &rel)
		report.Issues = append(report.Issues, fmt.Sprintf("orphan edge: %s -[%s]-> %s", src, rel, dst))
	}
	embRows, err := db.Query(`SELECT id, length(embedding) FROM entities WHERE archived = 0 AND embedding IS NOT NULL AND length(embedding) != ?`, vectorDim*4)
	if err != nil {
		return report, fmt.Errorf("verify dim: %w", err)
	}
	defer embRows.Close()
	for embRows.Next() {
		var id string
		var l int
		embRows.Scan(&id, &l)
		report.Issues = append(report.Issues, fmt.Sprintf("dimension mismatch: %s has %d bytes (want %d)", id, l, vectorDim*4))
	}
	return report, nil
}

// AgentLoop executes tasks in a goal's dependency tree in topological order.
func AgentLoop(ctx context.Context, db *sql.DB, schema core.SchemaConfig, goalID string, execFunc func(context.Context, core.Entity) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		tasks, err := retrieval.GetExecutableTasks(db, schema, goalID)
		if err != nil {
			return fmt.Errorf("agent loop: get executable: %w", err)
		}
		if len(tasks) == 0 {
			break
		}
		for _, task := range tasks {
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						slog.Error("agent loop: exec panic", "task_id", task.ID, "recover", rec)
					}
				}()
				if err := execFunc(ctx, task); err != nil {
					slog.Error("agent loop: exec", "task_id", task.ID, "error", err)
				}
			}()
			if err := store.SetStatus(db, schema, task.ID, schema.StateUnblocking); err != nil {
				return fmt.Errorf("agent loop: set status %s: %w", task.ID, err)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

// ExecutionPlan returns tasks for a goal.
func ExecutionPlan(db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Entity, error) {
	return retrieval.GetExecutableTasks(db, schema, goalID)
}
