package algo

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	taskdomain "github.com/pavelveter/hermem/src/internal/task"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// resolveExecutableTasks is the algorithm pkg's pointer to the task
// executable-tasks SQL. Used by AgentLoop + ExecutionPlan. PHASE 2.4
// redirection: previously called retrieval.GetExecutableTasks which
// used to live in retrieval/tasks.go; that file was deleted and the
// SQL migrated to internal/task/service.go.
func resolveExecutableTasks(ctx context.Context, db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Entity, error) {
	// taskdomain.NewService requires an embedder + vi; AgentLoop +
	// ExecutionPlan don't read either, so we pass nil. Service.Executable
	// never touches embedder or vi internally — same as retrieval's
	// pre-PHASE-2.4 GetExecutableTasks. The nil pointer is acceptable
	// because Service.Executable routes to getExecutable{,ForGoal,Global}
	// which only use s.db.
	svc := taskdomain.NewService(db, nil, nil)
	return svc.Executable(ctx, goalID, schema)
}

// AgentLoop executes tasks in a goal's dependency tree in topological order.
func AgentLoop(ctx context.Context, db *sql.DB, schema core.SchemaConfig, goalID string, execFunc func(context.Context, core.Entity) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		tasks, err := resolveExecutableTasks(ctx, db, schema, goalID)
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
func ExecutionPlan(ctx context.Context, db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Entity, error) {
	return resolveExecutableTasks(ctx, db, schema, goalID)
}
