package episodic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// EventType is the closed enum of event categories. The CHECK
// constraint in migration 011 enforces the same set at the SQL
// layer; this Go type mirrors it so callers get a typed API
// instead of free-form strings.
type EventType string

const (
	EventMessage     EventType = "message"
	EventAction      EventType = "action"
	EventObservation EventType = "observation"
	EventSystem      EventType = "system"
)

// validEventTypes is the lookup set used to validate Type before
// INSERT. Keeps the SQL CHECK constraint as the authoritative
// guard but returns a typed error (not a generic SQLite constraint
// failure) so callers can branch cleanly.
var validEventTypes = map[EventType]bool{
	EventMessage:     true,
	EventAction:      true,
	EventObservation: true,
	EventSystem:      true,
}

// Event is a single fine-grained episodic signal — one occurrence
// during an Episode. A user message, an assistant action, an
// observation noted by the system, or a system-generated event all
// fit the same shape (ID, EpisodeID, Type, Content, Timestamp,
// Metadata).
type Event struct {
	ID        string         `json:"id"`
	EpisodeID string         `json:"episode_id"`
	Type      EventType      `json:"type"`
	Content   string         `json:"content"`
	Timestamp time.Time      `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// EventService is the Event domain API. Flat-package + stateless
// pattern from timeline/ + evolution/.
type EventService struct {
	db *sql.DB
}

// NewEventService constructs an EventService. db is required.
func NewEventService(db *sql.DB) *EventService {
	return &EventService{db: db}
}

// CreateEvent inserts a new event. Validates ID, EpisodeID, and
// Type at the Go layer (the SQL CHECK constraint is the
// authoritative guard, but a typed error here is friendlier to
// callers than parsing a SQLite constraint failure).
//
// If event.Timestamp is zero it defaults to time.Now() so the
// timeline ordering has a stable anchor even when callers don't
// supply an explicit time.
func (s *EventService) CreateEvent(ctx context.Context, event Event) error {
	if event.ID == "" {
		return fmt.Errorf("episodic: CreateEvent: id required")
	}
	if event.EpisodeID == "" {
		return fmt.Errorf("episodic: CreateEvent: episode_id required")
	}
	if !validEventTypes[event.Type] {
		return fmt.Errorf("episodic: CreateEvent: invalid type %q (want message|action|observation|system)", event.Type)
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	meta := event.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("episodic: CreateEvent marshal metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO events (id, episode_id, type, content, timestamp, metadata)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		event.ID, event.EpisodeID, string(event.Type), event.Content, event.Timestamp, string(metaJSON),
	)
	if err != nil {
		return fmt.Errorf("episodic: CreateEvent insert: %w", err)
	}
	return nil
}

// ListEventsByEpisode returns all events for the given episode,
// ordered by timestamp ASC then id (stable tiebreak).
func (s *EventService) ListEventsByEpisode(ctx context.Context, episodeID string) ([]Event, error) {
	if episodeID == "" {
		return nil, fmt.Errorf("episodic: ListEventsByEpisode: episode_id required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, episode_id, type, content, timestamp, metadata
		 FROM events WHERE episode_id = ?
		 ORDER BY timestamp ASC, id ASC`, episodeID)
	if err != nil {
		return nil, fmt.Errorf("episodic: ListEventsByEpisode query: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: ListEventsByEpisode rows: %w", err)
	}
	if out == nil {
		out = []Event{}
	}
	return out, nil
}

// ListEventsByType returns events of the given type across all
// episodes, ordered by timestamp DESC (most-recent first). limit
// ≤ 0 means no cap.
func (s *EventService) ListEventsByType(ctx context.Context, eventType EventType, limit int) ([]Event, error) {
	if !validEventTypes[eventType] {
		return nil, fmt.Errorf("episodic: ListEventsByType: invalid type %q", eventType)
	}
	q := `SELECT id, episode_id, type, content, timestamp, metadata
	      FROM events WHERE type = ?
	      ORDER BY timestamp DESC, id ASC`
	args := []any{string(eventType)}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("episodic: ListEventsByType query: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: ListEventsByType rows: %w", err)
	}
	if out == nil {
		out = []Event{}
	}
	return out, nil
}

// scanEvent reads one event row from a *sql.Rows iterator.
func scanEvent(rows *sql.Rows) (*Event, error) {
	var ev Event
	var typ, metaJSON string
	if err := rows.Scan(&ev.ID, &ev.EpisodeID, &typ, &ev.Content, &ev.Timestamp, &metaJSON); err != nil {
		return nil, fmt.Errorf("episodic: scan event: %w", err)
	}
	ev.Type = EventType(typ)
	if err := json.Unmarshal([]byte(metaJSON), &ev.Metadata); err != nil {
		return nil, fmt.Errorf("episodic: scan event unmarshal metadata: %w", err)
	}
	if ev.Metadata == nil {
		ev.Metadata = map[string]any{}
	}
	return &ev, nil
}
