package evolution

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/memory/belief"
)

// ListActiveBeliefs returns only Active beliefs (excludes Superseded
// and Archived). When includeSuperseded is true, returns all beliefs
// regardless of status.
//
// The active-only filter is the default; includeSuperseded must be
// explicitly set to opt into the full set (e.g. for reconciliation
// or audit).
func ListActiveBeliefs(ctx context.Context, db *sql.DB, includeSuperseded bool) ([]*belief.Belief, error) {
	bSvc := belief.NewService(db)

	if includeSuperseded {
		return bSvc.ListBeliefs(ctx)
	}

	// Default filter: only Active.
	return scanFiltered(ctx, db, "status = ?", belief.StatusActive)
}

// scanFiltered queries beliefs with a WHERE clause.
func scanFiltered(ctx context.Context, db *sql.DB, where string, args ...any) ([]*belief.Belief, error) {
	q := `SELECT id, content, confidence, source_kind, source_id, status,
	       created_at, updated_at, superseded_by, parent_chain_id,
	       archived_at, last_accessed_at
	FROM beliefs WHERE ` + where + ` ORDER BY id ASC`

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("evolution: query: %w", err)
	}
	defer rows.Close()

	var out []*belief.Belief
	for rows.Next() {
		b, err := scanBelief(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evolution: rows: %w", err)
	}
	return out, nil
}

// scanBelief scans a row into a belief.Belief. Duplicates belief.scanRow
// to avoid importing unexported helpers.
func scanBelief(rows *sql.Rows) (*belief.Belief, error) {
	var b belief.Belief
	var sk, sid sql.NullString
	var supBy, parent sql.NullInt64
	var archived, acc sql.NullTime

	if err := rows.Scan(
		&b.ID, &b.Content, &b.Confidence, &sk, &sid, &b.Status,
		&b.CreatedAt, &b.UpdatedAt, &supBy, &parent,
		&archived, &acc,
	); err != nil {
		return nil, fmt.Errorf("evolution: scan belief: %w", err)
	}
	b.SourceKind = sk.String
	b.SourceID = sid.String
	if supBy.Valid {
		v := supBy.Int64
		b.SupersededBy = &v
	}
	if parent.Valid {
		v := parent.Int64
		b.ParentChainID = &v
	}
	if archived.Valid {
		v := archived.Time
		b.ArchivedAt = &v
	}
	if acc.Valid {
		v := acc.Time
		b.LastAccessedAt = &v
	}
	return &b, nil
}
