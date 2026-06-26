package evolution

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// GetSupersededBy returns the successor Belief ID for a superseded belief,
// or 0 if the belief is active or not found.
//
// Uses a single SQL query (no per-row loops).
func GetSupersededBy(ctx context.Context, db *sql.DB, beliefID int64) (int64, error) {
	if beliefID <= 0 {
		return 0, fmt.Errorf("evolution: invalid belief ID %d", beliefID)
	}
	var supersededBy sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT superseded_by FROM beliefs WHERE id = ?`, beliefID).Scan(&supersededBy)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("evolution: get superseded_by: %w", err)
	}
	if supersededBy.Valid {
		return supersededBy.Int64, nil
	}
	return 0, nil
}

// StateAt returns the state of a belief's revision chain as it existed
// at a given point in time by walking history entries. Returns nil when
// no history entry falls within the time window.
//
// Uses a single SQL query: find the most recent history entry before or
// at the given timestamp.
func StateAt(ctx context.Context, db *sql.DB, beliefID int64, at time.Time) (*HistoryEntry, error) {
	if beliefID <= 0 {
		return nil, fmt.Errorf("evolution: invalid belief ID %d", beliefID)
	}
	var h HistoryEntry
	var reason sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id, belief_id, confidence, status, COALESCE(reason, ''), created_at
		FROM belief_history
		WHERE belief_id = ? AND created_at <= ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, beliefID, at).Scan(&h.ID, &h.BeliefID, &h.Confidence, &h.Status, &reason, &h.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("evolution: state at: %w", err)
	}
	if reason.Valid {
		h.Reason = reason.String
	}
	return &h, nil
}
