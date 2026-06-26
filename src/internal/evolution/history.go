package evolution

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// HistoryEntry is an append-only record of a belief mutation.
// Once written it must never be modified (append-only invariant).
type HistoryEntry struct {
	ID         int64     `json:"id"`
	BeliefID   int64     `json:"belief_id"`
	Confidence float64   `json:"confidence"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// RecordHistory appends an entry to the belief_history table.
// This is the single mutation point for history — rows are INSERT-only.
func RecordHistory(ctx context.Context, db *sql.DB, beliefID int64, confidence float64, status, reason string) error {
	if beliefID <= 0 {
		return fmt.Errorf("evolution: invalid belief ID %d", beliefID)
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO belief_history (belief_id, confidence, status, reason)
		VALUES (?, ?, ?, ?)
	`, beliefID, confidence, status, nullStr(reason))
	if err != nil {
		return fmt.Errorf("evolution: record history: %w", err)
	}
	return nil
}

// ListHistory returns all history entries for a belief, ordered by
// created_at ASC (oldest first).
func ListHistory(ctx context.Context, db *sql.DB, beliefID int64) ([]HistoryEntry, error) {
	if beliefID <= 0 {
		return nil, fmt.Errorf("evolution: invalid belief ID %d", beliefID)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, belief_id, confidence, status, COALESCE(reason, ''), created_at
		FROM belief_history
		WHERE belief_id = ?
		ORDER BY created_at ASC, id ASC
	`, beliefID)
	if err != nil {
		return nil, fmt.Errorf("evolution: list history: %w", err)
	}
	defer rows.Close()

	var out []HistoryEntry
	for rows.Next() {
		var h HistoryEntry
		if err := rows.Scan(&h.ID, &h.BeliefID, &h.Confidence, &h.Status, &h.Reason, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("evolution: scan history: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evolution: rows: %w", err)
	}
	return out, nil
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
