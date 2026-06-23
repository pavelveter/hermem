package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// ExecutionPlan returns a topologically ordered list of tasks in the
// dependency tree rooted at goalID. Tasks with no remaining blockers
// come first; each subsequent layer requires the previous layer to
// be completed.
//
// The plan walks the blocked_by tree recursively (CTE), collects all
// tasks, then orders them by:
//  1. Leaf tasks (no blockers) first — these are immediately executable.
//  2. Tasks whose blockers are all in previous layers.
//
// Returns nil slice when goalID has no task tree.
func ExecutionPlan(db *sql.DB, schema SchemaConfig, goalID string) ([]Entity, error) {
	if goalID == "" {
		return nil, fmt.Errorf("goal_id required for execution plan")
	}
	if !schema.StatefulEnabled {
		return nil, fmt.Errorf("stateful schema not enabled")
	}

	// Collect all tasks in the dependency tree via recursive CTE.
	catPH, catArgs := boolMapInClause(schema.StatefulCategories)
	rel := schema.RelationBlocking
	args := append([]interface{}{goalID}, catArgs...)
	args = append(args, rel, rel)
	args = append(args, catArgs...)

	query := fmt.Sprintf(`
		WITH RECURSIVE dep_tree AS (
			SELECT e.id, e.category, e.content, e.status, e.updated_at, 0 as layer
			FROM entities e
			WHERE e.id = ? AND e.category IN (%s) AND e.archived = 0

			UNION ALL

			SELECT e.id, e.category, e.content, e.status, e.updated_at, dt.layer + 1
			FROM dep_tree dt
			JOIN edges ed ON ed.source_id = dt.id AND ed.relation_type = ?
			JOIN entities e ON e.id = ed.target_id AND e.category IN (%s) AND e.archived = 0
		)
		SELECT DISTINCT dt.id, dt.category, dt.content, dt.status, dt.updated_at
		FROM dep_tree dt
		ORDER BY dt.layer DESC, dt.id ASC
	`, catPH, catPH)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("execution plan for %s: %w", goalID, err)
	}
	defer rows.Close()

	var tasks []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &e.Status, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan execution plan: %w", err)
		}
		tasks = append(tasks, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution plan: %w", err)
	}
	return tasks, nil
}

// ExecuteNext finds the next executable task in the dependency tree
// rooted at goalID, transitions it to 'running', and returns it.
// If no executable task remains, returns nil with nil error.
//
// An executable task is one whose status is 'pending' (the first valid
// state) and whose all blocked_by targets have status = state_unblocking.
func ExecuteNext(db *sql.DB, schema SchemaConfig, goalID string) (*Entity, error) {
	tasks, err := GetExecutableTasks(db, schema, goalID)
	if err != nil {
		return nil, fmt.Errorf("execute next: %w", err)
	}
	if len(tasks) == 0 {
		return nil, nil
	}

	// Take the first executable task (deterministic: ordered by id).
	next := tasks[0]

	// Transition to running.
	runningState := nextValidState(schema, next.Status)
	if runningState == "" {
		return nil, fmt.Errorf("no valid transition from status %q for task %s",
			next.Status, next.ID)
	}

	if err := UpdateTaskStatus(db, schema, next.ID, runningState); err != nil {
		return nil, fmt.Errorf("transition task %s to %s: %w", next.ID, runningState, err)
	}

	next.Status = runningState
	slog.Info("task execution started",
		"event", "task_running",
		"task_id", next.ID,
		"status", runningState,
	)

	return &next, nil
}

// ExecuteComplete marks a task as completed (or the next valid state
// after its current status). Called after the agent has finished
// executing the task's work.
//
// After the transition, dependents (tasks blocked_by this one) may
// become executable. The caller should call ExecuteComplete for each
// completed task, then call ExecuteNext to get the next one.
func ExecuteComplete(db *sql.DB, schema SchemaConfig, taskID string) error {
	// Read current status to determine next state.
	var currentStatus string
	err := db.QueryRow(
		`SELECT status FROM entities WHERE id = ?`, taskID,
	).Scan(&currentStatus)
	if err != nil {
		return fmt.Errorf("execute complete: read status for %s: %w", taskID, err)
	}

	nextState := nextValidState(schema, currentStatus)
	if nextState == "" {
		return fmt.Errorf("no valid transition from status %q for task %s",
			currentStatus, taskID)
	}

	if err := UpdateTaskStatus(db, schema, taskID, nextState); err != nil {
		return fmt.Errorf("execute complete: transition %s to %s: %w",
			taskID, nextState, err)
	}

	slog.Info("task execution completed",
		"event", "task_completed",
		"task_id", taskID,
		"status", nextState,
	)
	return nil
}

// ExecuteFail marks a task as failed and looks up its recovery task.
// Returns the rollback task ID (empty if none configured).
func ExecuteFail(db *sql.DB, schema SchemaConfig, taskID string) (string, error) {
	if err := UpdateTaskStatus(db, schema, taskID, schema.ValidStateOrder[len(schema.ValidStateOrder)-1]); err != nil {
		return "", fmt.Errorf("execute fail: %w", err)
	}

	rollbackID, err := FindRollbackTask(db, schema, taskID)
	if err != nil {
		return "", fmt.Errorf("execute fail: lookup rollback: %w", err)
	}

	slog.Info("task execution failed",
		"event", "task_failed",
		"task_id", taskID,
		"rollback_task_id", rollbackID,
	)
	return rollbackID, nil
}

// AgentLoop runs the task execution loop for a given goal. It
// repeatedly calls ExecuteNext, executes the task (via the supplied
// callback), then calls ExecuteComplete. The loop stops when no
// more executable tasks remain or the context is cancelled.
//
// The callback receives each task and returns an error if execution
// failed. On failure, the task is marked as failed and the loop
// continues with the next executable (rollback is returned but not
// automatically executed — the caller decides how to handle it).
func AgentLoop(
	ctx context.Context,
	db *sql.DB,
	schema SchemaConfig,
	goalID string,
	execute func(ctx context.Context, task Entity) error,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		task, err := ExecuteNext(db, schema, goalID)
		if err != nil {
			return fmt.Errorf("agent loop: %w", err)
		}
		if task == nil {
			slog.Info("agent loop complete: no more executable tasks",
				"event", "agent_loop_done",
				"goal_id", goalID,
			)
			return nil
		}

		if err := execute(ctx, *task); err != nil {
			slog.Error("task execution failed",
				"event", "agent_loop_task_error",
				"task_id", task.ID,
				"err", err,
			)
			rollbackID, failErr := ExecuteFail(db, schema, task.ID)
			if failErr != nil {
				slog.Error("failed to mark task as failed",
					"event", "agent_loop_fail_error",
					"task_id", task.ID,
					"err", failErr,
				)
			}
			if rollbackID != "" {
				slog.Info("rollback task available",
					"event", "agent_loop_rollback",
					"task_id", task.ID,
					"rollback_id", rollbackID,
				)
			}
			// Continue with next executable — don't abort the entire
			// goal because one task failed.
			continue
		}

		if err := ExecuteComplete(db, schema, task.ID); err != nil {
			return fmt.Errorf("agent loop: execute complete: %w", err)
		}
	}
}

// nextValidState returns the next state after currentStatus in the
// configured ValidStateOrder. Used by ExecuteNext/ExecuteComplete to
// auto-advance the state machine.
func nextValidState(schema SchemaConfig, currentStatus string) string {
	if !schema.StatefulEnabled || len(schema.ValidStateOrder) == 0 {
		return ""
	}
	for i, s := range schema.ValidStateOrder {
		if s == currentStatus && i+1 < len(schema.ValidStateOrder) {
			return schema.ValidStateOrder[i+1]
		}
	}
	return ""
}
