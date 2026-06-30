package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
)

const defaultCascadeLimit = 4096

// StatusRolledBack is the fallback terminal status when
// schema.StateUnblocking is empty.
const StatusRolledBack = "rolled_back"

// ErrCascadeLimit is returned when a cascade rollback exceeds the
// configured depth/edge cap. The partial result is still valid.
var ErrCascadeLimit = errors.New("cascade rollback limit exceeded")

// GenerateRecoveryPlan walks the recovers_via chain forward from a failed task,
// returning the ordered list of recovery tasks to execute.
// A cycle in the recovers_via graph is broken at the second visit to any task
// by explicitly checking visited[rollbackID] before each append — prevents
// looping a→b→c→a from re-including the failed task `a` at the tail.
func GenerateRecoveryPlan(db *sql.DB, schema core.SchemaConfig, failedTaskID string) ([]core.Task, error) {
	plan := make([]core.Task, 0)
	visited := make(map[string]bool)
	current := failedTaskID
	for current != "" && !visited[current] {
		visited[current] = true
		rollbackID, err := FindRollbackTask(db, schema, current)
		if err != nil {
			return nil, fmt.Errorf("recovery plan: step from %s: %w", current, err)
		}
		if rollbackID == "" {
			break
		}
		if visited[rollbackID] {
			break
		}
		e, err := GetTaskByID(db, schema, rollbackID)
		if err != nil {
			return nil, fmt.Errorf("recovery plan: get task %s: %w", rollbackID, err)
		}
		plan = append(plan, e)
		current = rollbackID
	}
	return plan, nil
}

// FindRollbackTask looks up the recovers_via edge from a failed task.
// (kept here for clarity — recovery logic + its primitive in one place.)
func FindRollbackTask(db *sql.DB, schema core.SchemaConfig, failedTaskID string) (string, error) {
	var targetID string
	err := db.QueryRow(`SELECT ed.target_id FROM edges ed WHERE ed.source_id = ? AND ed.relation_type = ? LIMIT 1`, failedTaskID, schema.RelationRecovery).Scan(&targetID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find rollback task: %w", err)
	}
	return targetID, nil
}

// CascadeRollback rolls back a failed task and all tasks blocked-by it
// (transitive dependents). The errorContext is appended to each rolled-back
// task's content as a "[ROLLBACK: ...]" annotation. Already-rolled-back
// tasks are skipped (idempotent).
//
// Cycle safety: a visited set prevents infinite loops when blocked_by
// edges form a cycle.
//
// Depth safety: a hard cap (schema.CascadeLimit, default 4096) limits
// total tasks processed per invocation. Returns ErrCascadeLimit when
// exceeded — the partial result up to the cap is still returned.
//
// Returns the list of tasks that were rolled back (root + dependents).
// Partial failure does not abort the cascade — errored branches are
// skipped and the partial result is returned alongside the first error.
func CascadeRollback(db *sql.DB, schema core.SchemaConfig, id, errorContext string) ([]core.Task, error) {
	limit := schema.CascadeLimit
	if limit <= 0 {
		limit = defaultCascadeLimit
	}

	visited := make(map[string]bool)
	var result []core.Task
	var firstErr error

	// BFS queue — replaces recursive calls.
	queue := []string{id}

	for len(queue) > 0 {
		if len(result) >= limit {
			return result, fmt.Errorf("%w: %d tasks", ErrCascadeLimit, limit)
		}

		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		// Already rolled back? Skip entirely (including dependents).
		task, err := GetTaskByID(db, schema, current)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("cascade rollback: get %s: %w", current, err)
			}
			continue
		}
		if task.Status == schema.StateUnblocking {
			continue
		}

		// Annotate content with error context.
		newContent := task.Content
		if errorContext != "" {
			newContent = task.Content + "\n[ROLLBACK: " + errorContext + "]"
		}

		unblocking := schema.StateUnblocking
		if unblocking == "" {
			unblocking = StatusRolledBack
		}

		if _, err := db.Exec(`UPDATE entities SET status = ?, content = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			unblocking, newContent, current); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("cascade rollback: update %s: %w", current, err)
			}
			continue
		}

		task.Status = unblocking
		task.Content = newContent
		result = append(result, task)

		// Enqueue dependents for next BFS level.
		dependents, err := GetDependents(db, schema, current)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("cascade rollback: dependents of %s: %w", current, err)
			}
			continue
		}
		for _, edge := range dependents {
			if !visited[edge.TargetID] {
				queue = append(queue, edge.TargetID)
			}
		}
	}

	return result, firstErr
}
