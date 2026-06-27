// Package episodic owns the P2 EPISODIC MEMORY subsystem — the rich
// episode / event / session / link model that sits on top of the
// existing core.Entity thin wrappers and the sessions/conversations
// tables from migration 004.
//
// Following the flat-package + stateless-Service pattern from
// timeline/ and evolution/, this file owns the Episode domain type
// and its Service. Sibling files (session.go, event.go, linking.go,
// timeline.go, retrieval.go, summarization.go, playback.go) own the
// rest of the subsystem and share the same db connection passed to
// the per-area Service constructors.
package episodic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	hermemtime "github.com/pavelveter/hermem/src/internal/util/time"
)

// ErrNotFound is returned by Get* / List* methods when the requested
// id does not exist. Callers branch with errors.Is rather than
// string-matching.
var ErrNotFound = errors.New("episodic: not found")

// Episode is the rich P2 episodic memory unit. Distinct from
// core.Episode (the thin Entity-projection wrapper in
// src/internal/core/episode.go) — this type carries identity,
// timeline anchor, summary, and lifecycle fields directly, no
// conversion round-trip required.
//
// JSON tags mirror the column names so the type can be (de)serialized
// as a wire shape by future transport shells without an extra
// conversion step. time.Time is rendered RFC3339 for portability.
//
// Storage: StartedAt / EndedAt are persisted as INTEGER Unix
// milliseconds (UTC) in episodes.started_at_ms and
// episodes.ended_at_ms (introduced by migration 013). The
// hermemtime helpers enforce .UTC() on the write path so a
// non-UTC value cannot land in the column.
type Episode struct {
	ID             string         `json:"id"`
	SessionID      string         `json:"session_id,omitempty"`
	ConversationID string         `json:"conversation_id,omitempty"`
	Title          string         `json:"title"`
	Summary        string         `json:"summary"`
	StartedAt      time.Time      `json:"started_at"`
	EndedAt        *time.Time     `json:"ended_at,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// Service is the Episode domain API. Stateless — db is the only
// dependency. Constructed once per request-burst or held long-lived
// by transport shells.
type Service struct {
	db *sql.DB
}

// New constructs an Episode Service. db is required; nothing else.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// CreateEpisode inserts a new episode. If ep.StartedAt is zero it is
// defaulted to time.Now() so the timeline index has a stable anchor.
// Metadata is JSON-encoded; nil maps render as "{}" to match the
// migration default.
//
// Timestamp storage: StartedAt / EndedAt are persisted as INTEGER
// Unix milliseconds (UTC) via hermemtime.UnixMillisFromTime, which
// forces .UTC() on the value. Write-time TZ normalisation is the
// invariant — readers can rely on every stored ms value having been
// UTC at insert time.
func (s *Service) CreateEpisode(ctx context.Context, ep Episode) error {
	if ep.ID == "" {
		return fmt.Errorf("episodic: CreateEpisode: id required")
	}
	if ep.StartedAt.IsZero() {
		ep.StartedAt = time.Now()
	}
	meta := ep.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("episodic: CreateEpisode marshal metadata: %w", err)
	}
	startedAtMs := hermemtime.UnixMillisFromTime(ep.StartedAt)
	var endedAtMs sql.NullInt64
	if ep.EndedAt != nil {
		endedAtMs = sql.NullInt64{Int64: hermemtime.UnixMillisFromTime(*ep.EndedAt), Valid: true}
	}
	var sessionID, convID sql.NullString
	if ep.SessionID != "" {
		sessionID = sql.NullString{String: ep.SessionID, Valid: true}
	}
	if ep.ConversationID != "" {
		convID = sql.NullString{String: ep.ConversationID, Valid: true}
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO episodes (id, session_id, conversation_id, title, summary, started_at_ms, ended_at_ms, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ep.ID, sessionID, convID, ep.Title, ep.Summary, startedAtMs, endedAtMs, string(metaJSON),
	)
	if err != nil {
		return fmt.Errorf("episodic: CreateEpisode insert: %w", err)
	}
	return nil
}

// GetEpisode fetches an episode by id. Returns ErrNotFound when the
// row does not exist so callers can branch cleanly without scanning
// error messages.
func (s *Service) GetEpisode(ctx context.Context, id string) (*Episode, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, conversation_id, title, summary, started_at_ms, ended_at_ms, metadata
		 FROM episodes WHERE id = ?`, id)
	return scanEpisode(row)
}

// ListBySession returns all episodes attached to the given session,
// ordered by started_at ASC. limit ≤ 0 means no cap.
func (s *Service) ListBySession(ctx context.Context, sessionID string, limit int) ([]Episode, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("episodic: ListBySession: session_id required")
	}
	q := `SELECT id, session_id, conversation_id, title, summary, started_at_ms, ended_at_ms, metadata
	      FROM episodes WHERE session_id = ? ORDER BY started_at_ms ASC`
	args := []any{sessionID}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("episodic: ListBySession query: %w", err)
	}
	defer rows.Close()
	var out []Episode
	for rows.Next() {
		ep, err := scanEpisodeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: ListBySession rows: %w", err)
	}
	return core.NormalizeSlice(out), nil
}

// UpdateSummary overwrites the episode's summary column. Returns
// ErrNotFound if the id does not exist (rows affected = 0 is
// reported explicitly so callers can distinguish "missing" from
// "real error").
func (s *Service) UpdateSummary(ctx context.Context, id, summary string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE episodes SET summary = ? WHERE id = ?`, summary, id)
	if err != nil {
		return fmt.Errorf("episodic: UpdateSummary update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("episodic: UpdateSummary rows-affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// EndEpisode stamps ended_at_ms on the episode. Pass a zero time
// to mean "right now" — keeps call sites short for the common
// case. The value is persisted as INTEGER Unix milliseconds
// (UTC) via hermemtime.UnixMillisFromTime; the .UTC() call inside
// that helper enforces the write-time TZ invariant.
func (s *Service) EndEpisode(ctx context.Context, id string, endedAt time.Time) error {
	if endedAt.IsZero() {
		endedAt = time.Now()
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE episodes SET ended_at_ms = ? WHERE id = ?`,
		hermemtime.UnixMillisFromTime(endedAt), id)
	if err != nil {
		return fmt.Errorf("episodic: EndEpisode update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("episodic: EndEpisode rows-affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- shared scan helpers ---

// scanEpisode reads one episode row from a *sql.Row (QueryRow path).
// The two timestamp columns are persisted as INTEGER Unix milliseconds
// (UTC) by hermemtime.UnixMillisFromTime; readers convert back to
// time.Time via hermemtime.TimeFromUnixMillis. The returned time.Time
// is rendered as zero-valued for unset / zero-ms rows (matches the
// INSERT-side default of 0).
func scanEpisode(row *sql.Row) (*Episode, error) {
	var ep Episode
	var sessionID, convID sql.NullString
	var title, summary, metaJSON string
	var startedAtMs int64
	var endedAtMs sql.NullInt64
	if err := row.Scan(&ep.ID, &sessionID, &convID, &title, &summary, &startedAtMs, &endedAtMs, &metaJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("episodic: scan row: %w", err)
	}
	if sessionID.Valid {
		ep.SessionID = sessionID.String
	}
	if convID.Valid {
		ep.ConversationID = convID.String
	}
	ep.Title = title
	ep.Summary = summary
	ep.StartedAt = hermemtime.TimeFromUnixMillis(startedAtMs)
	if endedAtMs.Valid {
		t := hermemtime.TimeFromUnixMillis(endedAtMs.Int64)
		ep.EndedAt = &t
	}
	if err := json.Unmarshal([]byte(metaJSON), &ep.Metadata); err != nil {
		return nil, fmt.Errorf("episodic: scan unmarshal metadata: %w", err)
	}
	if ep.Metadata == nil {
		ep.Metadata = map[string]any{}
	}
	return &ep, nil
}

// scanEpisodeRows reads one episode row from a *sql.Rows iterator
// (ListBySession / multi-row paths). Same column set as scanEpisode.
func scanEpisodeRows(rows *sql.Rows) (*Episode, error) {
	var ep Episode
	var sessionID, convID sql.NullString
	var title, summary, metaJSON string
	var startedAtMs int64
	var endedAtMs sql.NullInt64
	if err := rows.Scan(&ep.ID, &sessionID, &convID, &title, &summary, &startedAtMs, &endedAtMs, &metaJSON); err != nil {
		return nil, fmt.Errorf("episodic: scan rows: %w", err)
	}
	if sessionID.Valid {
		ep.SessionID = sessionID.String
	}
	if convID.Valid {
		ep.ConversationID = convID.String
	}
	ep.Title = title
	ep.Summary = summary
	ep.StartedAt = hermemtime.TimeFromUnixMillis(startedAtMs)
	if endedAtMs.Valid {
		t := hermemtime.TimeFromUnixMillis(endedAtMs.Int64)
		ep.EndedAt = &t
	}
	if err := json.Unmarshal([]byte(metaJSON), &ep.Metadata); err != nil {
		return nil, fmt.Errorf("episodic: scan unmarshal metadata: %w", err)
	}
	if ep.Metadata == nil {
		ep.Metadata = map[string]any{}
	}
	return &ep, nil
}
