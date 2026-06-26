package episodic

import (
	"context"
	"database/sql"
	"fmt"
)

// TaskLinkService owns the many-to-many links between Episodes and
// task entities (the entities table where category = 'task').
// Separate from LinkService because the junction table
// (episode_tasks) has no role column — task links are flat, not
// categorised.
//
// Same flat-package + stateless pattern as the rest of episodic.
type TaskLinkService struct {
	db *sql.DB
}

// NewTaskLinkService constructs a TaskLinkService. db is required.
func NewTaskLinkService(db *sql.DB) *TaskLinkService {
	return &TaskLinkService{db: db}
}

// LinkTask inserts a (episode_id, task_id) link. Idempotent —
// ON CONFLICT DO NOTHING makes a duplicate link a no-op rather
// than a unique-constraint error. Both ids must be non-empty.
func (s *TaskLinkService) LinkTask(ctx context.Context, episodeID, taskID string) error {
	if episodeID == "" {
		return fmt.Errorf("episodic: LinkTask: episode_id required")
	}
	if taskID == "" {
		return fmt.Errorf("episodic: LinkTask: task_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO episode_tasks (episode_id, task_id) VALUES (?, ?)
		 ON CONFLICT (episode_id, task_id) DO NOTHING`,
		episodeID, taskID)
	if err != nil {
		return fmt.Errorf("episodic: LinkTask insert: %w", err)
	}
	return nil
}

// UnlinkTask removes the (episode_id, task_id) link. Idempotent:
// deleting a non-existent link returns nil (rows-affected = 0 is
// not an error). Both ids must be non-empty.
func (s *TaskLinkService) UnlinkTask(ctx context.Context, episodeID, taskID string) error {
	if episodeID == "" {
		return fmt.Errorf("episodic: UnlinkTask: episode_id required")
	}
	if taskID == "" {
		return fmt.Errorf("episodic: UnlinkTask: task_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM episode_tasks WHERE episode_id = ? AND task_id = ?`,
		episodeID, taskID)
	if err != nil {
		return fmt.Errorf("episodic: UnlinkTask delete: %w", err)
	}
	return nil
}

// TaskRef is the slim projection of a task entity returned by
// ListTasksForEpisode. Carries id, title (from content for now —
// task entities don't have a separate title column), status, and
// the link timestamp.
//
// Distinct from MemoryRef/EpisodeRef to keep the wire shape
// explicit at call sites.
type TaskRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	LinkedAt string `json:"linked_at"`
}

// ListTasksForEpisode returns all tasks linked to the given
// episode, ordered by linked_at ASC then task id (stable). Empty
// episode_id is rejected at the Go layer before the SQL runs.
func (s *TaskLinkService) ListTasksForEpisode(ctx context.Context, episodeID string) ([]TaskRef, error) {
	if episodeID == "" {
		return nil, fmt.Errorf("episodic: ListTasksForEpisode: episode_id required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.content, COALESCE(e.status, ''), et.linked_at
		 FROM episode_tasks et
		 JOIN entities e ON e.id = et.task_id
		 WHERE et.episode_id = ?
		 ORDER BY et.linked_at ASC, et.task_id ASC`, episodeID)
	if err != nil {
		return nil, fmt.Errorf("episodic: ListTasksForEpisode query: %w", err)
	}
	defer rows.Close()
	var out []TaskRef
	for rows.Next() {
		var t TaskRef
		if err := rows.Scan(&t.ID, &t.Title, &t.Status, &t.LinkedAt); err != nil {
			return nil, fmt.Errorf("episodic: ListTasksForEpisode scan: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: ListTasksForEpisode rows: %w", err)
	}
	if out == nil {
		out = []TaskRef{}
	}
	return out, nil
}
