// Package belief delivers the first-class schema-backed Belief abstraction for
// the P2 \u2014 MEMORY EVOLUTION subsystem (C1).
//
// It is intentionally separate from the existing core.Belief projection, which
// remains a thin view off of core.Entity for backward compatibility. This
// package owns the canonical persistence shape for evolving, confidence-scoring,
// support/refute-linked beliefs: it has its own SQLite table, lifecycle
// (Active / Superseded / Archived), and CRUD service.
package belief

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a Belief cannot be located.
var ErrNotFound = errors.New("belief: not found")

// Lifecycle statuses for a Belief.
const (
	StatusActive     = "Active"
	StatusSuperseded = "Superseded"
	StatusArchived   = "Archived"
)

// Belief is the schema-backed domain object for the Memory Evolution
// subsystem.
type Belief struct {
	ID             int64
	Content        string
	Confidence     float64
	SourceKind     string
	SourceID       string
	Status         string
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
	SupersededBy   *int64
	ParentChainID  *int64
	ArchivedAt     *time.Time
	LastAccessedAt *time.Time
}

// Service is the CRUD surface for Beliefs.
type Service interface {
	CreateBelief(ctx context.Context, b *Belief) error
	GetBelief(ctx context.Context, id int64) (*Belief, error)
	ListBeliefs(ctx context.Context) ([]*Belief, error)
	UpdateConfidence(ctx context.Context, id int64, conf float64) error
	MarkSuperseded(ctx context.Context, id int64, byID int64) error
}

// NewService returns a Service backed by the supplied SQLite database.
func New(db *sql.DB) Service {
	return &service{db: db}
}

type service struct {
	db *sql.DB
}

// CreateBelief assigns b.ID, b.CreatedAt, b.UpdatedAt and stores the row.
// Defaults: empty Status becomes StatusActive; non-positive Confidence
// becomes 1.0; empty Content is rejected; out-of-range Confidence is
// rejected with a domain error (not a SQLite string leak).
func (s *service) CreateBelief(ctx context.Context, b *Belief) error {
	if b == nil {
		return errors.New("belief: nil belief")
	}
	if b.Content == "" {
		return errors.New("belief: empty content")
	}
	if b.Confidence < 0 || b.Confidence > 1 {
		return fmt.Errorf("belief: confidence %f outside [0,1]", b.Confidence)
	}
	if b.Status == "" {
		b.Status = StatusActive
	}
	// Asymmetric defaults across create vs update:
	//
	// - CreateBelief silently maps Confidence == 0 to 1.0 (warm, forgiving).
	// - UpdateConfidence accepts 0 strictly (0 is a meaningful magnitude,
	//   e.g. retracting a belief to zero confidence). Bounds are tight:
	//   < 0 or > 1 is rejected; no silent default.
	//
	// The asymmetry is deliberate and documented to prevent drift.
	// to prevent future drift into symmetric, but wrong, defaults.
	if b.Confidence <= 0 {
		b.Confidence = 1.0
	}
	now := time.Now().UTC()
	b.CreatedAt = &now
	b.UpdatedAt = &now
	const q = `
INSERT INTO beliefs (content, confidence, source_kind, source_id, status,
                     created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
`
	res, err := s.db.ExecContext(ctx, q,
		b.Content, b.Confidence, nullStr(b.SourceKind), nullStr(b.SourceID),
		b.Status, b.CreatedAt, b.UpdatedAt)
	if err != nil {
		return fmt.Errorf("belief: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("belief: lastinsertid: %w", err)
	}
	b.ID = id
	return nil
}

// GetBelief returns the Belief with the given ID or ErrNotFound.
func (s *service) GetBelief(ctx context.Context, id int64) (*Belief, error) {
	if id <= 0 {
		return nil, ErrNotFound
	}
	rows, err := s.scan(ctx, "WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("belief: id %d matched %d rows", id, len(rows))
	}
	return rows[0], nil
}

// ListBeliefs returns every Belief across all lifecycle statuses,
// ordered by ID ascending.
func (s *service) ListBeliefs(ctx context.Context) ([]*Belief, error) {
	return s.scan(ctx, "")
}

func (s *service) scan(ctx context.Context, where string, args ...any) ([]*Belief, error) {
	q := `
SELECT id, content, confidence, source_kind, source_id, status,
       created_at, updated_at, superseded_by, parent_chain_id,
       archived_at, last_accessed_at
FROM beliefs
` + where + `
ORDER BY id ASC
`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("belief: query: %w", err)
	}
	defer rows.Close()
	var out []*Belief
	for rows.Next() {
		b, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("belief: rows: %w", err)
	}
	return out, nil
}

// UpdateConfidence sets a new confidence in [0,1] and bumps
// updated_at + last_accessed_at. Returns ErrNotFound if the ID is missing
// or non-positive.
func (s *service) UpdateConfidence(ctx context.Context, id int64, conf float64) error {
	if id <= 0 {
		return ErrNotFound
	}
	if conf < 0 || conf > 1 {
		return fmt.Errorf("belief: confidence %f outside [0,1]", conf)
	}
	now := time.Now().UTC()
	const q = `
UPDATE beliefs
   SET confidence = ?, updated_at = ?, last_accessed_at = ?
 WHERE id = ?
`
	res, err := s.db.ExecContext(ctx, q, conf, now, now, id)
	if err != nil {
		return fmt.Errorf("belief: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("belief: rowsaffected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkSuperseded transitions a Belief to StatusSuperseded and links it to a
// successor Belief via SupersededBy. Wrapped in a single transaction;
// non-positive IDs return ErrNotFound short-circuit.
func (s *service) MarkSuperseded(ctx context.Context, id, byID int64) error {
	if id <= 0 || byID <= 0 {
		return ErrNotFound
	}
	if id == byID {
		return errors.New("belief: superseded_by must differ from id")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("belief: begin: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM beliefs WHERE id = ?`, byID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("belief: target %d not found", byID)
	}
	if err != nil {
		return fmt.Errorf("belief: lookup target: %w", err)
	}

	now := time.Now().UTC()
	const q = `
UPDATE beliefs
   SET status        = 'Superseded',
       superseded_by = ?,
       archived_at   = ?,
       updated_at    = ?
 WHERE id = ?
`
	res, err := tx.ExecContext(ctx, q, byID, now, now, id)
	if err != nil {
		return fmt.Errorf("belief: mark superseded: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("belief: rowsaffected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("belief: commit: %w", err)
	}
	return nil
}

// scanRow hydrates a Belief from a 12-column SELECT.
func scanRow(rows *sql.Rows) (*Belief, error) {
	var b Belief
	var (
		sk, sid       sql.NullString
		supBy, parent sql.NullInt64
		archived, acc sql.NullTime
	)
	if err := rows.Scan(
		&b.ID, &b.Content, &b.Confidence, &sk, &sid, &b.Status,
		&b.CreatedAt, &b.UpdatedAt, &supBy, &parent,
		&archived, &acc,
	); err != nil {
		return nil, fmt.Errorf("belief: scan: %w", err)
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

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
