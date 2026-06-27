package episodic

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// openTestDB returns an in-memory SQLite with the episodic schema
// applied. Uses mattn/go-sqlite3 directly (via the blank import)
// so the test does not depend on the store package's migration
// runner — keeps the episodic package independently testable.
//
// Two tables are minimal: episodes (for Episode CRUD) plus the
// sessions table (FK target for episodes.session_id). The other
// migration-011 tables (events, episode_memories, episode_tasks)
// are created lazily by the test files that need them.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		ended_at DATETIME,
		metadata TEXT DEFAULT '{}'
	)`,
		`CREATE TABLE IF NOT EXISTS conversations (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		summary TEXT DEFAULT '',
		metadata TEXT DEFAULT '{}')`,
		`CREATE TABLE IF NOT EXISTS episodes (
			id TEXT PRIMARY KEY,
			session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
			conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,
			title TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			started_at_ms INTEGER NOT NULL DEFAULT 0,
			ended_at_ms INTEGER,
			metadata TEXT NOT NULL DEFAULT '{}'
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec schema: %v\n%s", err, s)
		}
	}
	return db
}

// seedSession inserts a minimal sessions row so episodes can FK to it.
func seedSession(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO sessions (id) VALUES (?)`, id); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func TestService_CreateEpisode_DefaultsStartedAtAndMetadata(t *testing.T) {
	db := openTestDB(t)
	svc := New(db)

	before := time.Now()
	if err := svc.CreateEpisode(t.Context(), Episode{
		ID:    "ep-1",
		Title: "first episode",
	}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}

	got, err := svc.GetEpisode(t.Context(), "ep-1")
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	if got.StartedAt.Before(before.Add(-time.Millisecond)) || got.StartedAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("StartedAt default out of range: %v", got.StartedAt)
	}
	if got.Metadata == nil {
		t.Fatal("Metadata: want non-nil empty map, got nil")
	}
	if len(got.Metadata) != 0 {
		t.Fatalf("Metadata: want empty, got %v", got.Metadata)
	}
}

func TestService_CreateEpisode_PreservesMetadata(t *testing.T) {
	db := openTestDB(t)
	svc := New(db)

	meta := map[string]any{
		"source": "test",
		"count":  float64(42), // JSON round-trips numbers as float64
		"active": true,
	}
	if err := svc.CreateEpisode(t.Context(), Episode{
		ID:        "ep-2",
		Title:     "metadata test",
		Metadata:  meta,
		StartedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}

	got, err := svc.GetEpisode(t.Context(), "ep-2")
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	if got.Metadata["source"] != "test" {
		t.Errorf("metadata.source: want test, got %v", got.Metadata["source"])
	}
	if got.Metadata["count"] != float64(42) {
		t.Errorf("metadata.count: want 42, got %v", got.Metadata["count"])
	}
	if got.Metadata["active"] != true {
		t.Errorf("metadata.active: want true, got %v", got.Metadata["active"])
	}
}

func TestService_CreateEpisode_RequiresID(t *testing.T) {
	db := openTestDB(t)
	svc := New(db)
	err := svc.CreateEpisode(t.Context(), Episode{Title: "no id"})
	if err == nil {
		t.Fatal("want error for empty id, got nil")
	}
	if !strings.Contains(err.Error(), "id required") {
		t.Fatalf("err must mention id requirement, got: %v", err)
	}
}

func TestService_GetEpisode_NotFound(t *testing.T) {
	db := openTestDB(t)
	svc := New(db)
	_, err := svc.GetEpisode(t.Context(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestService_UpdateSummary_Persists(t *testing.T) {
	db := openTestDB(t)
	svc := New(db)
	if err := svc.CreateEpisode(t.Context(), Episode{
		ID:    "ep-3",
		Title: "summary test",
	}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	if err := svc.UpdateSummary(t.Context(), "ep-3", "new summary"); err != nil {
		t.Fatalf("UpdateSummary: %v", err)
	}
	got, err := svc.GetEpisode(t.Context(), "ep-3")
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	if got.Summary != "new summary" {
		t.Fatalf("Summary: want 'new summary', got %q", got.Summary)
	}
}

func TestService_UpdateSummary_NotFound(t *testing.T) {
	db := openTestDB(t)
	svc := New(db)
	err := svc.UpdateSummary(t.Context(), "missing", "x")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestService_EndEpisode_StampsEndedAt(t *testing.T) {
	db := openTestDB(t)
	svc := New(db)
	started := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	if err := svc.CreateEpisode(t.Context(), Episode{
		ID:        "ep-4",
		StartedAt: started,
	}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	if err := svc.EndEpisode(t.Context(), "ep-4", end); err != nil {
		t.Fatalf("EndEpisode: %v", err)
	}
	got, err := svc.GetEpisode(t.Context(), "ep-4")
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	if got.EndedAt == nil {
		t.Fatal("EndedAt: want non-nil after EndEpisode")
	}
	if !got.EndedAt.Equal(end) {
		t.Fatalf("EndedAt: want %v, got %v", end, *got.EndedAt)
	}
}

func TestService_EndEpisode_NotFound(t *testing.T) {
	db := openTestDB(t)
	svc := New(db)
	err := svc.EndEpisode(t.Context(), "missing", time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestService_ListBySession_OrdersByStartedAt(t *testing.T) {
	db := openTestDB(t)
	seedSession(t, db, "sess-1")
	svc := New(db)

	// Insert out of order; ListBySession must return them sorted ASC.
	eps := []Episode{
		{ID: "ep-c", SessionID: "sess-1", StartedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), Title: "third"},
		{ID: "ep-a", SessionID: "sess-1", StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Title: "first"},
		{ID: "ep-b", SessionID: "sess-1", StartedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Title: "second"},
	}
	for _, e := range eps {
		if err := svc.CreateEpisode(t.Context(), e); err != nil {
			t.Fatalf("CreateEpisode %s: %v", e.ID, err)
		}
	}

	got, err := svc.ListBySession(t.Context(), "sess-1", 0)
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	wantIDs := []string{"ep-a", "ep-b", "ep-c"}
	if len(got) != len(wantIDs) {
		t.Fatalf("len: want %d, got %d (%v)", len(wantIDs), len(got), got)
	}
	for i, e := range got {
		if e.ID != wantIDs[i] {
			t.Errorf("position %d: want %s, got %s", i, wantIDs[i], e.ID)
		}
	}
}

func TestService_ListBySession_FiltersBySession(t *testing.T) {
	db := openTestDB(t)
	seedSession(t, db, "sess-1")
	seedSession(t, db, "sess-2")
	svc := New(db)

	for _, e := range []Episode{
		{ID: "ep-1", SessionID: "sess-1", StartedAt: time.Now(), Title: "one"},
		{ID: "ep-2", SessionID: "sess-1", StartedAt: time.Now(), Title: "two"},
		{ID: "ep-3", SessionID: "sess-2", StartedAt: time.Now(), Title: "three"},
	} {
		if err := svc.CreateEpisode(t.Context(), e); err != nil {
			t.Fatalf("CreateEpisode %s: %v", e.ID, err)
		}
	}

	got, err := svc.ListBySession(t.Context(), "sess-1", 0)
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("sess-1: want 2 episodes, got %d", len(got))
	}
	for _, e := range got {
		if e.SessionID != "sess-1" {
			t.Errorf("got episode %s from session %s; want sess-1", e.ID, e.SessionID)
		}
	}
}

func TestService_ListBySession_EmptyReturnsEmptySlice(t *testing.T) {
	db := openTestDB(t)
	seedSession(t, db, "sess-empty")
	svc := New(db)
	got, err := svc.ListBySession(t.Context(), "sess-empty", 0)
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want 0 episodes, got %d", len(got))
	}
}

func TestService_ListBySession_RequiresSessionID(t *testing.T) {
	db := openTestDB(t)
	svc := New(db)
	_, err := svc.ListBySession(t.Context(), "", 0)
	if err == nil {
		t.Fatal("want error for empty session_id, got nil")
	}
}

func TestService_ListBySession_Limit(t *testing.T) {
	db := openTestDB(t)
	seedSession(t, db, "sess-1")
	svc := New(db)
	for i := 0; i < 5; i++ {
		id := []string{"e1", "e2", "e3", "e4", "e5"}[i]
		if err := svc.CreateEpisode(t.Context(), Episode{
			ID:        id,
			SessionID: "sess-1",
			StartedAt: time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
			Title:     id,
		}); err != nil {
			t.Fatalf("CreateEpisode %s: %v", id, err)
		}
	}
	got, err := svc.ListBySession(t.Context(), "sess-1", 2)
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("limit=2: want 2, got %d", len(got))
	}
	if got[0].ID != "e1" || got[1].ID != "e2" {
		t.Fatalf("limit order: want [e1,e2], got [%s,%s]", got[0].ID, got[1].ID)
	}
}
