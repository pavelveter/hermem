package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// StoreEntityWithEmbedding persists an entity to SQLite and mirrors its embedding into the vector index.
func StoreEntityWithEmbedding(db *sql.DB, vi core.VectorIndex, schema core.SchemaConfig, entity core.Entity) error {
	var embeddingBytes []byte
	hasEmbedding := len(entity.Embedding) > 0
	if hasEmbedding {
		embeddingBytes = EmbeddingToBytes(entity.Embedding)
	}

	if hasEmbedding {
		if err := vi.Store(context.Background(), entity.ID, entity.Embedding); err != nil {
			return fmt.Errorf("vector index store: %w", err)
		}
	}

	status := entity.Status
	if status == "" && schema.StatefulCategories[entity.Category] && len(schema.ValidStateOrder) > 0 {
		status = schema.ValidStateOrder[0]
	}
	_, err := db.Exec(`INSERT OR REPLACE INTO entities (id, category, content, embedding, updated_at, status) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		entity.ID, entity.Category, entity.Content, embeddingBytes, NullString(status))
	if err != nil {
		if hasEmbedding {
			if rmErr := vi.Remove(context.Background(), []string{entity.ID}); rmErr != nil {
				slog.Warn("vector index rollback after sqlite failure", "event", "vector_rollback_fail", "entity_id", entity.ID, "rm_err", rmErr)
			}
		}
		return err
	}
	return nil
}

// NullString returns nil for empty string, otherwise the value (for SQL NULL handling).
func NullString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

// OrNullTime returns nil for nil *time.Time, otherwise the underlying value.
func OrNullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}

// SetStatus updates a stateful entity's status column.
func SetStatus(db *sql.DB, schema core.SchemaConfig, id, status string) error {
	if !schema.ValidStates[status] {
		return fmt.Errorf("invalid status: %s", status)
	}
	res, err := db.Exec(`UPDATE entities SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("stateful entity not found: %s", id)
	}
	return nil
}

// GetStatus returns the status of an entity.
func GetStatus(db *sql.DB, id string) (string, error) {
	var status sql.NullString
	err := db.QueryRow(`SELECT status FROM entities WHERE id = ?`, id).Scan(&status)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get task status: %w", err)
	}
	if !status.Valid {
		return "", nil
	}
	return status.String, nil
}

// InClauseArgs builds N "?" placeholders and an args slice for SQL IN (...) queries.
func InClauseArgs(ids []string) (string, []interface{}) {
	phs := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		phs[i] = "?"
		args[i] = id
	}
	return joinPhs(phs), args
}

func joinPhs(phs []string) string {
	if len(phs) == 0 {
		return ""
	}
	s := phs[0]
	for i := 1; i < len(phs); i++ {
		s += "," + phs[i]
	}
	return s
}

// BoolMapInClause builds placeholders and args for a map of bool keys.
func BoolMapInClause(values map[string]bool) (string, []interface{}) {
	keys := SortedKeys(values)
	ph := make([]string, len(keys))
	args := make([]interface{}, len(keys))
	for i, key := range keys {
		ph[i] = "?"
		args[i] = key
	}
	return joinPhs(ph), args
}
