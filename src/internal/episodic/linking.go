package episodic

import (
	"context"
	"database/sql"
	"fmt"
)

// LinkRole is the default role used when callers pass an empty
// string to LinkMemory. The CHECK constraint at the SQL layer is
// permissive (no enum on role), so any string is allowed; this
// constant just gives the common case a name.
const LinkRoleExtracted = "extracted"

// LinkService owns the many-to-many links between Episodes and
// the entities table (memory facts, beliefs, tasks). The two
// junction tables (episode_memories + episode_tasks) come from
// migration 011; this file splits their APIs into two cohesive
// Services so callers can depend on just the surface they need.
//
// Linking is flat-package + stateless, same pattern as the rest
// of the episodic package.
type LinkService struct {
	db *sql.DB
}

// NewLinkService constructs a LinkService. db is required.
func NewLinkService(db *sql.DB) *LinkService {
	return &LinkService{db: db}
}

// LinkMemory inserts a (episode_id, entity_id, role) link. ON
// CONFLICT DO NOTHING makes the operation idempotent — linking
// the same triple twice is a no-op rather than an error.
//
// role="" defaults to LinkRoleExtracted ("extracted") so callers
// that don't care about role semantics can pass the empty string.
// All three of episodeID / entityID / role must be non-empty after
// the default is applied.
func (s *LinkService) LinkMemory(ctx context.Context, episodeID, entityID, role string) error {
	if episodeID == "" {
		return fmt.Errorf("episodic: LinkMemory: episode_id required")
	}
	if entityID == "" {
		return fmt.Errorf("episodic: LinkMemory: entity_id required")
	}
	if role == "" {
		role = LinkRoleExtracted
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO episode_memories (episode_id, entity_id, role) VALUES (?, ?, ?)
		 ON CONFLICT (episode_id, entity_id, role) DO NOTHING`,
		episodeID, entityID, role)
	if err != nil {
		return fmt.Errorf("episodic: LinkMemory insert: %w", err)
	}
	return nil
}

// UnlinkMemory removes the (episode_id, entity_id, role) link. role
// "" defaults to LinkRoleExtracted so callers can match the
// LinkMemory-without-role default. Idempotent: deleting a non-
// existent link returns nil (rows-affected = 0 is not an error).
func (s *LinkService) UnlinkMemory(ctx context.Context, episodeID, entityID, role string) error {
	if episodeID == "" {
		return fmt.Errorf("episodic: UnlinkMemory: episode_id required")
	}
	if entityID == "" {
		return fmt.Errorf("episodic: UnlinkMemory: entity_id required")
	}
	if role == "" {
		role = LinkRoleExtracted
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM episode_memories WHERE episode_id = ? AND entity_id = ? AND role = ?`,
		episodeID, entityID, role)
	if err != nil {
		return fmt.Errorf("episodic: UnlinkMemory delete: %w", err)
	}
	return nil
}

// MemoryRef is the slim projection of an entity returned by
// ListMemoriesForEpisode. Full Entity carries 16 fields most of
// which are irrelevant to episode callers (e.g. Degree / Priority);
// this projection keeps the JSON wire shape tight.
//
// Distinct from core.Entity to make the intent explicit at call
// sites — ListMemoriesForEpisode returns []MemoryRef, not
// []core.Entity.
//
// BREAKING: LinkedAt was previously a string (RFC3339-ish
// formatter); after migration 013 it is an int64 Unix millisecond
// (UTC) read from episode_memories.linked_at_ms. Clients parsing
// the field as time.Time need to switch to time.UnixMilli(c).
type MemoryRef struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Content  string `json:"content"`
	Role     string `json:"role"`
	LinkedAt int64  `json:"linked_at"`
}

// ListMemoriesForEpisode returns all entities linked to the given
// episode, with their link role and timestamp. Ordered by link
// timestamp_ms ASC then entity id (stable).
func (s *LinkService) ListMemoriesForEpisode(ctx context.Context, episodeID string) ([]MemoryRef, error) {
	if episodeID == "" {
		return nil, fmt.Errorf("episodic: ListMemoriesForEpisode: episode_id required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.category, e.content, em.role, em.linked_at_ms
		 FROM episode_memories em
		 JOIN entities e ON e.id = em.entity_id
		 WHERE em.episode_id = ?
		 ORDER BY em.linked_at_ms ASC, em.entity_id ASC`, episodeID)
	if err != nil {
		return nil, fmt.Errorf("episodic: ListMemoriesForEpisode query: %w", err)
	}
	defer rows.Close()
	out := make([]MemoryRef, 0)
	for rows.Next() {
		var m MemoryRef
		if err := rows.Scan(&m.ID, &m.Category, &m.Content, &m.Role, &m.LinkedAt); err != nil {
			return nil, fmt.Errorf("episodic: ListMemoriesForEpisode scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: ListMemoriesForMemory rows: %w", err)
	}
	return out, nil
}

// EpisodeRef is the slim projection of an episode returned by
// ListEpisodesForMemory. Same projection discipline as MemoryRef;
// LinkedAt is INTEGER Unix milliseconds (UTC) after migration 013.
type EpisodeRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Role     string `json:"role"`
	LinkedAt int64  `json:"linked_at"`
}

// ListEpisodesForMemory returns all episodes linked to the given
// entity, with their link role and timestamp. Ordered by link
// timestamp_ms ASC then episode id (stable).
func (s *LinkService) ListEpisodesForMemory(ctx context.Context, entityID string) ([]EpisodeRef, error) {
	if entityID == "" {
		return nil, fmt.Errorf("episodic: ListEpisodesForMemory: entity_id required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT ep.id, ep.title, ep.summary, em.role, em.linked_at_ms
		 FROM episode_memories em
		 JOIN episodes ep ON ep.id = em.episode_id
		 WHERE em.entity_id = ?
		 ORDER BY em.linked_at_ms ASC, em.episode_id ASC`, entityID)
	if err != nil {
		return nil, fmt.Errorf("episodic: ListEpisodesForMemory query: %w", err)
	}
	defer rows.Close()
	out := make([]EpisodeRef, 0)
	for rows.Next() {
		var e EpisodeRef
		if err := rows.Scan(&e.ID, &e.Title, &e.Summary, &e.Role, &e.LinkedAt); err != nil {
			return nil, fmt.Errorf("episodic: ListEpisodesForMemory scan: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("episodic: ListEpisodesForMemory rows: %w", err)
	}
	return out, nil
}
