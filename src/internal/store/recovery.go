package store

import (
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
)

// GenerateRecoveryPlan walks the recovers_via chain forward from a failed task,
// returning the ordered list of recovery tasks to execute.
func GenerateRecoveryPlan(db *sql.DB, schema core.SchemaConfig, failedTaskID string) ([]core.Entity, error) {
	var plan []core.Entity
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
