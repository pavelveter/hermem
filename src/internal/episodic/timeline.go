package episodic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	hermemtime "github.com/pavelveter/hermem/src/internal/util/time"
)

// TimelineEntryKind is the closed enum of frame sources in a
// reconstructed timeline. Mirrors the three sources merged by
// ReconstructTimeline: events, linked memories, linked tasks.
type TimelineEntryKind string

const (
	TimelineEvent  TimelineEntryKind = "event"
	TimelineMemory TimelineEntryKind = "memory"
	TimelineTask   TimelineEntryKind = "task"
)

// validTimelineEntryKinds is the set of accepted TimelineEntryKind values.
var validTimelineEntryKinds = map[TimelineEntryKind]bool{
	TimelineEvent:  true,
	TimelineMemory: true,
	TimelineTask:   true,
}

// ErrInvalidTimelineEntryKind is returned when an unknown TimelineEntryKind
// is provided to UnmarshalText/UnmarshalJSON.
var ErrInvalidTimelineEntryKind = errors.New("episodic: invalid timeline entry kind")

// UnmarshalText implements encoding.TextUnmarshaler.
func (k *TimelineEntryKind) UnmarshalText(data []byte) error {
	v := TimelineEntryKind(data)
	if !validTimelineEntryKinds[v] {
		return fmt.Errorf("%w: %q", ErrInvalidTimelineEntryKind, string(data))
	}
	*k = v
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (k *TimelineEntryKind) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return k.UnmarshalText([]byte(s))
}

// TimelineEntry is one row of a reconstructed episode timeline.
// Kind discriminates the source so callers can render / filter
// by origin. Timestamp is the per-source time anchor:
//
//	event  → events.timestamp_ms        (INTEGER unix ms, UTC)
//	memory → episode_memories.linked_at_ms (INTEGER unix ms, UTC)
//	task   → episode_tasks.linked_at_ms    (INTEGER unix ms, UTC)
//
// All three are converted to time.Time via hermemtime so the
// chronological merge in ReconstructTimeline can compare them
// uniformly. Migration 013 introduced the *_ms columns.
type TimelineEntry struct {
	Kind      TimelineEntryKind `json:"kind"`
	SourceID  string            `json:"source_id"` // event id / entity id / task id
	EpisodeID string            `json:"episode_id"`
	Timestamp time.Time         `json:"timestamp"`
	// Type is the event Type (message|action|observation|system) for
	// Kind=event; the link role (extracted|referenced|mentioned) for
	// Kind=memory; the task status for Kind=task. Empty otherwise.
	Type string `json:"type,omitempty"`
	// Content is the human-readable payload — event content, memory
	// content, or task content. Kept as a flat string so the wire
	// shape is uniform across kinds.
	Content string `json:"content"`
}

// TimelineService reconstructs chronological episode feeds.
// Flat-package + stateless pattern.
type TimelineService struct {
	db *sql.DB
}

// NewTimelineService constructs a TimelineService.
func NewTimelineService(db *sql.DB) *TimelineService {
	return &TimelineService{db: db}
}

// ReconstructTimeline returns the merged chronological feed for
// an episode: every event on the episode plus every linked memory
// and linked task, ordered by timestamp ASC. Stable tiebreak by
// (kind, source_id) so re-running on the same data returns rows
// in the same order.
//
// Empty episode_id is rejected. An episode with no events and no
// links returns a non-nil empty slice.
//
// The implementation runs three SELECTs (events on timestamp_ms,
// episode_memories on linked_at_ms, episode_tasks on
// linked_at_ms) and merges in Go — each query is small (scoped
// to one episode) and the in-memory merge keeps the chronological
// ordering simple. If a future caller hits perf issues on huge
// episodes, the merge can be pushed to SQL via UNION ALL with a
// shared ORDER BY on the ms columns.
func (s *TimelineService) ReconstructTimeline(ctx context.Context, episodeID string) ([]TimelineEntry, error) {
	if episodeID == "" {
		return nil, fmt.Errorf("episodic: ReconstructTimeline: episode_id required")
	}

	events, err := s.eventsForEpisode(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	memories, err := s.memoriesForEpisode(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	tasks, err := s.tasksForEpisode(ctx, episodeID)
	if err != nil {
		return nil, err
	}

	all := make([]TimelineEntry, 0, len(events)+len(memories)+len(tasks))
	all = append(all, events...)
	all = append(all, memories...)
	all = append(all, tasks...)

	// Chronological sort, stable on (kind, source_id) so the merge
	// is deterministic across runs.
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && (all[j].Timestamp.Before(all[j-1].Timestamp) ||
			(all[j].Timestamp.Equal(all[j-1].Timestamp) && entryLess(all[j], all[j-1]))); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	return all, nil
}

// entryLess is the stable tiebreak for ReconstructTimeline when
// timestamps collide: events < memories < tasks (kind ordering),
// then by source id lexicographic.
func entryLess(a, b TimelineEntry) bool {
	if a.Kind != b.Kind {
		return string(a.Kind) < string(b.Kind)
	}
	return a.SourceID < b.SourceID
}

// eventsForEpisode returns event-typed TimelineEntry rows for the
// episode, ordered by timestamp_ms ASC, id ASC. Each event
// contributes one row regardless of its Type. The stored column
// is INTEGER Unix milliseconds (UTC) introduced by migration 013
// — converted back to time.Time here so the merge in
// ReconstructTimeline can chronological-sort across all three
// sources uniformly.
func (s *TimelineService) eventsForEpisode(ctx context.Context, episodeID string) ([]TimelineEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, content, timestamp_ms FROM events
		 WHERE episode_id = ?
		 ORDER BY timestamp_ms ASC, id ASC`, episodeID)
	if err != nil {
		return nil, fmt.Errorf("episodic: eventsForEpisode: %w", err)
	}
	defer rows.Close()
	var out []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		var timestampMs int64
		if err := rows.Scan(&e.SourceID, &e.Type, &e.Content, &timestampMs); err != nil {
			return nil, fmt.Errorf("episodic: eventsForEpisode scan: %w", err)
		}
		e.Kind = TimelineEvent
		e.EpisodeID = episodeID
		e.Timestamp = hermemtime.TimeFromUnixMillis(timestampMs)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: eventsForEpisode rows: %w", err)
	}
	return out, nil
}

// memoriesForEpisode returns memory-typed TimelineEntry rows for
// the episode (one per link; the same entity can appear multiple
// times if linked under different roles). Reads
// episode_memories.linked_at_ms (INTEGER unix ms, UTC) via the
// hermemtime helper.
func (s *TimelineService) memoriesForEpisode(ctx context.Context, episodeID string) ([]TimelineEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.content, em.role, em.linked_at_ms
		 FROM episode_memories em
		 JOIN entities e ON e.id = em.entity_id
		 WHERE em.episode_id = ?
		 ORDER BY em.linked_at_ms ASC, em.entity_id ASC`, episodeID)
	if err != nil {
		return nil, fmt.Errorf("episodic: memoriesForEpisode: %w", err)
	}
	defer rows.Close()
	var out []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		var linkedAtMs int64
		if err := rows.Scan(&e.SourceID, &e.Content, &e.Type, &linkedAtMs); err != nil {
			return nil, fmt.Errorf("episodic: memoriesForEpisode scan: %w", err)
		}
		e.Kind = TimelineMemory
		e.EpisodeID = episodeID
		e.Timestamp = hermemtime.TimeFromUnixMillis(linkedAtMs)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: memoriesForEpisode rows: %w", err)
	}
	return out, nil
}

// tasksForEpisode returns task-typed TimelineEntry rows for the
// episode, one per link. Reads episode_tasks.linked_at_ms
// (INTEGER unix ms, UTC) via the hermemtime helper.
func (s *TimelineService) tasksForEpisode(ctx context.Context, episodeID string) ([]TimelineEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.content, COALESCE(e.status, ''), et.linked_at_ms
		 FROM episode_tasks et
		 JOIN entities e ON e.id = et.task_id
		 WHERE et.episode_id = ?
		 ORDER BY et.linked_at_ms ASC, et.task_id ASC`, episodeID)
	if err != nil {
		return nil, fmt.Errorf("episodic: tasksForEpisode: %w", err)
	}
	defer rows.Close()
	var out []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		var linkedAtMs int64
		if err := rows.Scan(&e.SourceID, &e.Content, &e.Type, &linkedAtMs); err != nil {
			return nil, fmt.Errorf("episodic: tasksForEpisode scan: %w", err)
		}
		e.Kind = TimelineTask
		e.EpisodeID = episodeID
		e.Timestamp = hermemtime.TimeFromUnixMillis(linkedAtMs)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: tasksForEpisode rows: %w", err)
	}
	return out, nil
}
