package episodic

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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

// TimelineEntry is one row of a reconstructed episode timeline.
// Kind discriminates the source so callers can render / filter by
// origin. Timestamp is the per-source time anchor:
//
//	event  → events.timestamp
//	memory → episode_memories.linked_at
//	task   → episode_tasks.linked_at
//
// All three use the same time.Time type so chronological merging
// is a direct comparison.
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
// The implementation runs three SELECTs (events, episode_memories,
// episode_tasks) and merges in Go — each query is small (scoped
// to one episode) and the in-memory merge keeps the chronological
// ordering simple. If a future caller hits perf issues on huge
// episodes, the merge can be pushed to SQL via UNION ALL with a
// shared ORDER BY.
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
// episode, ordered by timestamp ASC, id ASC. Each event contributes
// one row regardless of its Type.
func (s *TimelineService) eventsForEpisode(ctx context.Context, episodeID string) ([]TimelineEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, content, timestamp FROM events
		 WHERE episode_id = ?
		 ORDER BY timestamp ASC, id ASC`, episodeID)
	if err != nil {
		return nil, fmt.Errorf("episodic: eventsForEpisode: %w", err)
	}
	defer rows.Close()
	var out []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		if err := rows.Scan(&e.SourceID, &e.Type, &e.Content, &e.Timestamp); err != nil {
			return nil, fmt.Errorf("episodic: eventsForEpisode scan: %w", err)
		}
		e.Kind = TimelineEvent
		e.EpisodeID = episodeID
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: eventsForEpisode rows: %w", err)
	}
	return out, nil
}

// memoriesForEpisode returns memory-typed TimelineEntry rows for
// the episode (one per link; the same entity can appear multiple
// times if linked under different roles).
func (s *TimelineService) memoriesForEpisode(ctx context.Context, episodeID string) ([]TimelineEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.content, em.role, em.linked_at
		 FROM episode_memories em
		 JOIN entities e ON e.id = em.entity_id
		 WHERE em.episode_id = ?
		 ORDER BY em.linked_at ASC, em.entity_id ASC`, episodeID)
	if err != nil {
		return nil, fmt.Errorf("episodic: memoriesForEpisode: %w", err)
	}
	defer rows.Close()
	var out []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		if err := rows.Scan(&e.SourceID, &e.Content, &e.Type, &e.Timestamp); err != nil {
			return nil, fmt.Errorf("episodic: memoriesForEpisode scan: %w", err)
		}
		e.Kind = TimelineMemory
		e.EpisodeID = episodeID
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: memoriesForEpisode rows: %w", err)
	}
	return out, nil
}

// tasksForEpisode returns task-typed TimelineEntry rows for the
// episode, one per link.
func (s *TimelineService) tasksForEpisode(ctx context.Context, episodeID string) ([]TimelineEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.content, COALESCE(e.status, ''), et.linked_at
		 FROM episode_tasks et
		 JOIN entities e ON e.id = et.task_id
		 WHERE et.episode_id = ?
		 ORDER BY et.linked_at ASC, et.task_id ASC`, episodeID)
	if err != nil {
		return nil, fmt.Errorf("episodic: tasksForEpisode: %w", err)
	}
	defer rows.Close()
	var out []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		if err := rows.Scan(&e.SourceID, &e.Content, &e.Type, &e.Timestamp); err != nil {
			return nil, fmt.Errorf("episodic: tasksForEpisode scan: %w", err)
		}
		e.Kind = TimelineTask
		e.EpisodeID = episodeID
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: tasksForEpisode rows: %w", err)
	}
	return out, nil
}
