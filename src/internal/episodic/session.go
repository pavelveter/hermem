package episodic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// Session is the P2 EPISODIC MEMORY top-level container — a bounded
// time-window during which one or more conversations (and the
// episodes extracted from them) took place. The table already
// exists from migration 004_episodic_sessions.sql; this file adds
// the Go service layer that the schema has been missing.
//
// Sessions own no domain semantics of their own — they're a
// grouping primitive for episodes + conversations. The Service
// here is intentionally minimal: create / get / end / list.
type Session struct {
	ID        string         `json:"id"`
	StartedAt time.Time      `json:"started_at"`
	EndedAt   *time.Time     `json:"ended_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// SessionService is the Session domain API. Flat-package + stateless
// pattern from timeline/ + evolution/. Same db connection as the
// Episode Service so the two can be passed side-by-side to transport
// shells.
type SessionService struct {
	db *sql.DB
}

// NewSessionService constructs a SessionService. db is required;
// nothing else.
func NewSessionService(db *sql.DB) *SessionService {
	return &SessionService{db: db}
}

// CreateSession inserts a new session row. If session.StartedAt is
// zero it defaults to time.Now(). Nil metadata renders as "{}"
// so callers don't have to populate it.
func (s *SessionService) CreateSession(ctx context.Context, session Session) error {
	if session.ID == "" {
		return fmt.Errorf("episodic: CreateSession: id required")
	}
	if session.StartedAt.IsZero() {
		session.StartedAt = time.Now()
	}
	meta := session.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("episodic: CreateSession marshal metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, started_at, ended_at, metadata) VALUES (?, ?, ?, ?)`,
		session.ID, session.StartedAt, sql.NullTime{}, string(metaJSON),
	)
	if err != nil {
		return fmt.Errorf("episodic: CreateSession insert: %w", err)
	}
	return nil
}

// GetSession fetches a session by id. Returns ErrNotFound when the
// row does not exist.
func (s *SessionService) GetSession(ctx context.Context, id string) (*Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, started_at, ended_at, metadata FROM sessions WHERE id = ?`, id)
	var sess Session
	var metaJSON string
	var endedAt sql.NullTime
	if err := row.Scan(&sess.ID, &sess.StartedAt, &endedAt, &metaJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("episodic: GetSession scan: %w", err)
	}
	if endedAt.Valid {
		t := endedAt.Time
		sess.EndedAt = &t
	}
	if err := json.Unmarshal([]byte(metaJSON), &sess.Metadata); err != nil {
		return nil, fmt.Errorf("episodic: GetSession unmarshal metadata: %w", err)
	}
	if sess.Metadata == nil {
		sess.Metadata = map[string]any{}
	}
	return &sess, nil
}

// EndSession stamps ended_at on the session. Pass zero time to
// mean "right now". Idempotent — calling twice updates the same row.
func (s *SessionService) EndSession(ctx context.Context, id string, endedAt time.Time) error {
	if endedAt.IsZero() {
		endedAt = time.Now()
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET ended_at = ? WHERE id = ?`, endedAt, id)
	if err != nil {
		return fmt.Errorf("episodic: EndSession update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("episodic: EndSession rows-affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListSessions returns sessions ordered by started_at DESC (most
// recent first). limit ≤ 0 means no cap.
func (s *SessionService) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	q := `SELECT id, started_at, ended_at, metadata FROM sessions ORDER BY started_at DESC`
	args := []any{}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("episodic: ListSessions query: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		var metaJSON string
		var endedAt sql.NullTime
		if err := rows.Scan(&sess.ID, &sess.StartedAt, &endedAt, &metaJSON); err != nil {
			return nil, fmt.Errorf("episodic: ListSessions scan: %w", err)
		}
		if endedAt.Valid {
			t := endedAt.Time
			sess.EndedAt = &t
		}
		if err := json.Unmarshal([]byte(metaJSON), &sess.Metadata); err != nil {
			return nil, fmt.Errorf("episodic: ListSessions unmarshal metadata: %w", err)
		}
		if sess.Metadata == nil {
			sess.Metadata = map[string]any{}
		}
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: ListSessions rows: %w", err)
	}
	return core.NormalizeSlice(out), nil
}
