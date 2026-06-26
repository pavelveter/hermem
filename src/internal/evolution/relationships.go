package evolution

import (
	"context"
	"database/sql"
	"fmt"
)

// RelationshipCounts holds support/refute counts for a belief.
type RelationshipCounts struct {
	BeliefID    int64
	Support     int
	Refute      int
	Total       int
	SupportPct  float64
	RefutePct   float64
}

// GetSupportRefute counts evidence rows by polarity for one belief.
// Uses a single SQL query (not per-row loops) for N+1 safety.
func GetSupportRefute(ctx context.Context, db *sql.DB, beliefID int64) (RelationshipCounts, error) {
	if beliefID <= 0 {
		return RelationshipCounts{}, fmt.Errorf("evolution: invalid belief ID %d", beliefID)
	}

	q := `SELECT
		COALESCE(SUM(CASE WHEN polarity = 'support' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN polarity = 'refute' THEN 1 ELSE 0 END), 0),
		COUNT(*)
	FROM evidence WHERE belief_id = ?`

	var r RelationshipCounts
	r.BeliefID = beliefID
	err := db.QueryRowContext(ctx, q, beliefID).Scan(&r.Support, &r.Refute, &r.Total)
	if err != nil {
		return r, fmt.Errorf("evolution: support/refute: %w", err)
	}
	if r.Total > 0 {
		r.SupportPct = float64(r.Support) / float64(r.Total) * 100
		r.RefutePct = float64(r.Refute) / float64(r.Total) * 100
	}
	return r, nil
}
