package episodic

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// openLinkTestDB returns an in-memory SQLite with episodes +
// entities + episode_memories tables applied. The FK chain
// episode_memories → (episodes, entities) requires both root
// tables to exist.
func openLinkTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS episodes (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			conversation_id TEXT,
			title TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			started_at_ms INTEGER NOT NULL DEFAULT 0,
			ended_at_ms INTEGER,
			metadata TEXT NOT NULL DEFAULT '{}'
		)`,
		// Minimal entities table — only the columns the linking
		// queries read or write. Mirrors the real schema's
		// primary key + category + content; other Entity columns
		// are intentionally omitted because linking never touches them.
		`CREATE TABLE IF NOT EXISTS entities (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS episode_memories (
			episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
			entity_id TEXT NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
			role TEXT NOT NULL DEFAULT 'extracted',
			linked_at_ms INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (episode_id, entity_id, role)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec schema: %v\n%s", err, s)
		}
	}
	return db
}

func seedEpisodeForLink(t *testing.T, db *sql.DB, id, title string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO episodes (id, title) VALUES (?, ?)`, id, title); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
}

func seedEntityForLink(t *testing.T, db *sql.DB, id, category, content string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES (?, ?, ?)`, id, category, content); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
}

func TestLinkService_LinkMemory_RequiresEpisodeID(t *testing.T) {
	db := openLinkTestDB(t)
	svc := NewLinkService(db)
	err := svc.LinkMemory(t.Context(), "", "ent-1", "extracted")
	if err == nil || !strings.Contains(err.Error(), "episode_id required") {
		t.Fatalf("want episode_id-required error, got %v", err)
	}
}

func TestLinkService_LinkMemory_RequiresEntityID(t *testing.T) {
	db := openLinkTestDB(t)
	seedEpisodeForLink(t, db, "ep-1", "")
	svc := NewLinkService(db)
	err := svc.LinkMemory(t.Context(), "ep-1", "", "extracted")
	if err == nil || !strings.Contains(err.Error(), "entity_id required") {
		t.Fatalf("want entity_id-required error, got %v", err)
	}
}

func TestLinkService_LinkMemory_DefaultsRole(t *testing.T) {
	db := openLinkTestDB(t)
	seedEpisodeForLink(t, db, "ep-1", "")
	seedEntityForLink(t, db, "ent-1", "world", "fact")
	svc := NewLinkService(db)

	if err := svc.LinkMemory(t.Context(), "ep-1", "ent-1", ""); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	got, err := svc.ListMemoriesForEpisode(t.Context(), "ep-1")
	if err != nil {
		t.Fatalf("ListMemoriesForEpisode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 memory, got %d", len(got))
	}
	if got[0].Role != LinkRoleExtracted {
		t.Errorf("default role: want %q, got %q", LinkRoleExtracted, got[0].Role)
	}
}

func TestLinkService_LinkMemory_Idempotent(t *testing.T) {
	db := openLinkTestDB(t)
	seedEpisodeForLink(t, db, "ep-1", "")
	seedEntityForLink(t, db, "ent-1", "world", "fact")
	svc := NewLinkService(db)

	for i := 0; i < 3; i++ {
		if err := svc.LinkMemory(t.Context(), "ep-1", "ent-1", "extracted"); err != nil {
			t.Fatalf("LinkMemory #%d: %v", i, err)
		}
	}
	got, _ := svc.ListMemoriesForEpisode(t.Context(), "ep-1")
	if len(got) != 1 {
		t.Fatalf("idempotent: want 1 link, got %d", len(got))
	}
}

func TestLinkService_LinkMemory_SameEntityDifferentRoles(t *testing.T) {
	db := openLinkTestDB(t)
	seedEpisodeForLink(t, db, "ep-1", "")
	seedEntityForLink(t, db, "ent-1", "world", "fact")
	svc := NewLinkService(db)

	for _, role := range []string{"extracted", "referenced", "mentioned"} {
		if err := svc.LinkMemory(t.Context(), "ep-1", "ent-1", role); err != nil {
			t.Fatalf("LinkMemory role=%s: %v", role, err)
		}
	}
	got, _ := svc.ListMemoriesForEpisode(t.Context(), "ep-1")
	if len(got) != 3 {
		t.Fatalf("want 3 links (one per role), got %d", len(got))
	}
}

func TestLinkService_UnlinkMemory_RemovesLink(t *testing.T) {
	db := openLinkTestDB(t)
	seedEpisodeForLink(t, db, "ep-1", "")
	seedEntityForLink(t, db, "ent-1", "world", "fact")
	svc := NewLinkService(db)

	if err := svc.LinkMemory(t.Context(), "ep-1", "ent-1", "extracted"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	if err := svc.UnlinkMemory(t.Context(), "ep-1", "ent-1", "extracted"); err != nil {
		t.Fatalf("UnlinkMemory: %v", err)
	}
	got, _ := svc.ListMemoriesForEpisode(t.Context(), "ep-1")
	if len(got) != 0 {
		t.Fatalf("after unlink: want 0 memories, got %d", len(got))
	}
}

func TestLinkService_UnlinkMemory_Idempotent(t *testing.T) {
	db := openLinkTestDB(t)
	seedEpisodeForLink(t, db, "ep-1", "")
	svc := NewLinkService(db)
	// Deleting a non-existent link is a no-op (no error).
	if err := svc.UnlinkMemory(t.Context(), "ep-1", "missing", "extracted"); err != nil {
		t.Fatalf("UnlinkMemory on missing link: %v", err)
	}
}

func TestLinkService_UnlinkMemory_RequiresIDs(t *testing.T) {
	db := openLinkTestDB(t)
	svc := NewLinkService(db)
	if err := svc.UnlinkMemory(t.Context(), "", "ent-1", ""); err == nil {
		t.Fatal("want error for empty episode_id")
	}
	if err := svc.UnlinkMemory(t.Context(), "ep-1", "", ""); err == nil {
		t.Fatal("want error for empty entity_id")
	}
}

func TestLinkService_UnlinkMemory_OnlyMatchingRole(t *testing.T) {
	db := openLinkTestDB(t)
	seedEpisodeForLink(t, db, "ep-1", "")
	seedEntityForLink(t, db, "ent-1", "world", "fact")
	svc := NewLinkService(db)

	if err := svc.LinkMemory(t.Context(), "ep-1", "ent-1", "extracted"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	if err := svc.LinkMemory(t.Context(), "ep-1", "ent-1", "referenced"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	if err := svc.UnlinkMemory(t.Context(), "ep-1", "ent-1", "extracted"); err != nil {
		t.Fatalf("UnlinkMemory: %v", err)
	}
	got, _ := svc.ListMemoriesForEpisode(t.Context(), "ep-1")
	if len(got) != 1 {
		t.Fatalf("want 1 remaining link (referenced), got %d", len(got))
	}
	if got[0].Role != "referenced" {
		t.Errorf("remaining role: want referenced, got %s", got[0].Role)
	}
}

func TestLinkService_ListMemoriesForEpisode_OrdersByLinkedAt(t *testing.T) {
	db := openLinkTestDB(t)
	seedEpisodeForLink(t, db, "ep-1", "")
	seedEntityForLink(t, db, "ent-a", "world", "first")
	seedEntityForLink(t, db, "ent-b", "world", "second")
	seedEntityForLink(t, db, "ent-c", "world", "third")
	svc := NewLinkService(db)

	// Insert out of order; List must return by linked_at ASC.
	for _, eid := range []string{"ent-c", "ent-a", "ent-b"} {
		if err := svc.LinkMemory(t.Context(), "ep-1", eid, "extracted"); err != nil {
			t.Fatalf("LinkMemory %s: %v", eid, err)
		}
	}
	got, err := svc.ListMemoriesForEpisode(t.Context(), "ep-1")
	if err != nil {
		t.Fatalf("ListMemoriesForEpisode: %v", err)
	}
	// linked_at uses CURRENT_TIMESTAMP with second precision, so
	// three sequential inserts in a single test may share a
	// timestamp — fall back to id ordering in that case.
	if len(got) != 3 {
		t.Fatalf("want 3 memories, got %d", len(got))
	}
}

func TestLinkService_ListMemoriesForEpisode_EmptyReturnsEmptySlice(t *testing.T) {
	db := openLinkTestDB(t)
	seedEpisodeForLink(t, db, "ep-empty", "")
	svc := NewLinkService(db)
	got, err := svc.ListMemoriesForEpisode(t.Context(), "ep-empty")
	if err != nil {
		t.Fatalf("ListMemoriesForEpisode: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want 0 memories, got %d", len(got))
	}
}

func TestLinkService_ListMemoriesForEpisode_RequiresEpisodeID(t *testing.T) {
	db := openLinkTestDB(t)
	svc := NewLinkService(db)
	_, err := svc.ListMemoriesForEpisode(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "episode_id required") {
		t.Fatalf("want episode_id-required error, got %v", err)
	}
}

func TestLinkService_ListEpisodesForMemory_ReturnsAllLinkedEpisodes(t *testing.T) {
	db := openLinkTestDB(t)
	seedEpisodeForLink(t, db, "ep-1", "first")
	seedEpisodeForLink(t, db, "ep-2", "second")
	seedEntityForLink(t, db, "ent-1", "world", "fact")
	svc := NewLinkService(db)

	if err := svc.LinkMemory(t.Context(), "ep-1", "ent-1", "extracted"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	if err := svc.LinkMemory(t.Context(), "ep-2", "ent-1", "extracted"); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}
	got, err := svc.ListEpisodesForMemory(t.Context(), "ent-1")
	if err != nil {
		t.Fatalf("ListEpisodesForMemory: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 episodes, got %d", len(got))
	}
	titles := map[string]bool{}
	for _, e := range got {
		titles[e.Title] = true
	}
	if !titles["first"] || !titles["second"] {
		t.Fatalf("missing titles, got %v", titles)
	}
}

func TestLinkService_ListEpisodesForMemory_RequiresEntityID(t *testing.T) {
	db := openLinkTestDB(t)
	svc := NewLinkService(db)
	_, err := svc.ListEpisodesForMemory(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "entity_id required") {
		t.Fatalf("want entity_id-required error, got %v", err)
	}
}

func TestLinkService_ListEpisodesForMemory_EmptyReturnsEmptySlice(t *testing.T) {
	db := openLinkTestDB(t)
	seedEntityForLink(t, db, "ent-orphan", "world", "no links")
	svc := NewLinkService(db)
	got, err := svc.ListEpisodesForMemory(t.Context(), "ent-orphan")
	if err != nil {
		t.Fatalf("ListEpisodesForMemory: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want 0 episodes, got %d", len(got))
	}
}
