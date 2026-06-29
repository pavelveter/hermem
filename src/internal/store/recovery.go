package store

import (
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
)

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
// Cycle safety: a visited set prevents infinite recursion when blocked_by
// edges form a cycle.
//
// Returns the list of tasks that were rolled back (root + dependents).
// Partial failure does not abort the cascade — errored branches are
// skipped and the partial result is returned alongside the first error.
func CascadeRollback(db *sql.DB, schema core.SchemaConfig, id, errorContext string) ([]core.Task, error) {
	return cascadeRollback(db, schema, id, errorContext, make(map[string]bool))
}

func cascadeRollback(db *sql.DB, schema core.SchemaConfig, id, errorContext string, visited map[string]bool) ([]core.Task, error) {
	if visited[id] {
		return nil, nil
	}
	visited[id] = true

	// Already rolled back? Skip.
	task, err := GetTaskByID(db, schema, id)
	if err != nil {
		return nil, fmt.Errorf("cascade rollback: get %s: %w", id, err)
	}
	if task.Status == schema.StateUnblocking {
		return nil, nil
	}

	// Annotate content with error context.
	newContent := task.Content
	if errorContext != "" {
		newContent = task.Content + "\n[ROLLBACK: " + errorContext + "]"
	}

	unblocking := schema.StateUnblocking
	if unblocking == "" {
		unblocking = "rolled_back"
	}

	if _, err := db.Exec(`UPDATE entities SET status = ?, content = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		unblocking, newContent, id); err != nil {
		return nil, fmt.Errorf("cascade rollback: update %s: %w", id, err)
	}

	task.Status = unblocking
	task.Content = newContent
	result := []core.Task{task}

	// Find all tasks blocked-by this one.
	blocked, err := GetBlockedBy(db, schema, id)
	if err != nil {
		return result, fmt.Errorf("cascade rollback: dependents of %s: %w", id, err)
	}

	var firstErr error
	for _, edge := range blocked {
		sub, err := cascadeRollback(db, schema, edge.SourceID, errorContext, visited)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		result = append(result, sub...)
	}

	return result, firstErr
}
