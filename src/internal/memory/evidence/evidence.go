// Package evidence delivers the first-class Evidence abstraction for the
// P2 \u2014 MEMORY EVOLUTION track (C2). Evidence is a typed artifact that supports
// or refutes a Belief; it carries polarity, weight, content text, and optional
// source provenance.
//
// It is intentionally separate from belief.Belief: the relationship is data-
// driven (Evidence.belief_id REFERENCES beliefs.id) without import-time coupling.
// Confidence propagation in C3 will aggregate Evidence rows to update
// Belief.Confidence; C8 will surface support/refute reasoning. Until then
// this package is the schema-side ledger.
package evidence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Polarity tags an Evidence as supporting or refuting its target Belief.
// Strength is an absolute magnitude [0,1]; whether that magnitude adds to or
// subtracts from the Belief's confidence is decided by the aggregator (C3).
type Polarity string

const (
	PolaritySupport Polarity = "support"
	PolarityRefute  Polarity = "refute"
)

var (
	ErrNotFound        = errors.New("evidence: not found")
	ErrInvalidPolarity = errors.New("evidence: invalid polarity")
)

// Evidence is the canonical schema-backed representation.
type Evidence struct {
	ID         int64
	BeliefID   int64
	Polarity   Polarity
	Strength   float64
	Content    string
	SourceKind string
	SourceID   string
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
}

// Service is the persistence-side interface over the evidence table.
type Service interface {
	CreateEvidence(ctx context.Context, e *Evidence) error
	GetEvidence(ctx context.Context, id int64) (*Evidence, error)
	ListForBelief(ctx context.Context, beliefID int64) ([]*Evidence, error)
	UpdateStrength(ctx context.Context, id int64, newStrength float64) error
	DeleteEvidence(ctx context.Context, id int64) error
}

type sqlService struct {
	db *sql.DB
}

// NewService returns a SQL-backed Service over db.
func New(db *sql.DB) Service { return &sqlService{db: db} }

func (s *sqlService) CreateEvidence(ctx context.Context, e *Evidence) error {
	if e == nil {
		return errors.New("evidence: nil")
	}
	if e.Content == "" {
		return errors.New("evidence: empty content")
	}
	if e.Polarity != PolaritySupport && e.Polarity != PolarityRefute {
		return ErrInvalidPolarity
	}
	if e.Strength < 0 || e.Strength > 1 {
		return fmt.Errorf("evidence: strength %v outside [0,1]", e.Strength)
	}
	// Asymmetric defaults across create vs update:
	//
	// - CreateEvidence silently maps Strength == 0 to 1.0. Warm path,
	//   forgiving for callers who do not care about strength. Mirrors
	//   CreateBelief from C1.
	// - UpdateStrength accepts 0 strictly (0 is a meaningful magnitude
	//   like any other [0,1] value, e.g. retracting evidence to zero).
	//   Bounds are tight: < 0 or > 1 is rejected; no silent default.
	//
	// The asymmetry is deliberate; documented here to prevent drift.
	// create \u2014 mirrors C1's behavior with Belief.Confidence.
	// UpdateStrength below rejects 0 strictly so the asymmetry exists
	// only on the warm path (creation), not the strict path.
	if e.Strength == 0 {
		e.Strength = 1.0
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO evidence (belief_id, polarity, strength, content, source_kind, source_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, e.BeliefID, string(e.Polarity), e.Strength, e.Content, nullIfEmpty(e.SourceKind), nullIfEmpty(e.SourceID))
	if err != nil {
		return fmt.Errorf("evidence insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("evidence lastid: %w", err)
	}
	e.ID = id
	return nil
}

func (s *sqlService) GetEvidence(ctx context.Context, id int64) (*Evidence, error) {
	if id <= 0 {
		return nil, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, belief_id, polarity, strength, content, source_kind, source_id, created_at, updated_at
		FROM evidence WHERE id = ?
	`, id)
	return scanRow(row)
}

func (s *sqlService) ListForBelief(ctx context.Context, beliefID int64) ([]*Evidence, error) {
	if beliefID <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, belief_id, polarity, strength, content, source_kind, source_id, created_at, updated_at
		FROM evidence WHERE belief_id = ? ORDER BY created_at ASC, id ASC
	`, beliefID)
	if err != nil {
		return nil, fmt.Errorf("evidence list: %w", err)
	}
	defer rows.Close()
	var out []*Evidence
	for rows.Next() {
		var e Evidence
		var srcKind, srcID sql.NullString
		if err := rows.Scan(&e.ID, &e.BeliefID, &e.Polarity, &e.Strength, &e.Content, &srcKind, &srcID, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("evidence scan: %w", err)
		}
		e.SourceKind = srcKind.String
		e.SourceID = srcID.String
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evidence rows: %w", err)
	}
	return out, nil
}

func (s *sqlService) UpdateStrength(ctx context.Context, id int64, newStrength float64) error {
	if id <= 0 {
		return ErrNotFound
	}
	if newStrength < 0 || newStrength > 1 {
		return fmt.Errorf("evidence: strength %v outside [0,1]", newStrength)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE evidence SET strength = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, newStrength, id)
	if err != nil {
		return fmt.Errorf("evidence update: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("evidence rows-affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlService) DeleteEvidence(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrNotFound
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM evidence WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("evidence delete: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("evidence rows-affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func scanRow(row *sql.Row) (*Evidence, error) {
	var e Evidence
	var srcKind, srcID sql.NullString
	if err := row.Scan(&e.ID, &e.BeliefID, &e.Polarity, &e.Strength, &e.Content, &srcKind, &srcID, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("evidence scan: %w", err)
	}
	e.SourceKind = srcKind.String
	e.SourceID = srcID.String
	return &e, nil
}

// nullIfEmpty returns a NULL SQL value when s == ""; otherwise a valid value.
func nullIfEmpty(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
