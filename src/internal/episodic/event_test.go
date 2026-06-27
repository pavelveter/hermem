package episodic

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// openEventTestDB returns an in-memory SQLite with sessions,
// conversations, and episodes tables applied (the FK chain
// events → episodes → sessions/conversations). Tests seed what
// they need on top.
func openEventTestDB(t *testing.T) *sql.DB {
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
			metadata TEXT DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS episodes (
			id TEXT PRIMARY KEY,
			session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
			conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL,
			title TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ended_at DATETIME,
			metadata TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			episode_id TEXT NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
			type TEXT NOT NULL CHECK(type IN ('message', 'action', 'observation', 'system')),
			content TEXT NOT NULL DEFAULT '',
			timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
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

func seedEpisode(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO episodes (id) VALUES (?)`, id); err != nil {
		t.Fatalf("seed episode: %v", err)
	}
}

func TestEventService_CreateEvent_DefaultsTimestampAndMetadata(t *testing.T) {
	db := openEventTestDB(t)
	seedEpisode(t, db, "ep-1")
	svc := NewEventService(db)

	before := time.Now()
	if err := svc.CreateEvent(t.Context(), Event{
		ID:        "ev-1",
		EpisodeID: "ep-1",
		Type:      EventMessage,
		Content:   "hello",
	}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	got, err := svc.ListEventsByEpisode(t.Context(), "ep-1")
	if err != nil {
		t.Fatalf("ListEventsByEpisode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	if got[0].Timestamp.Before(before) || got[0].Timestamp.After(time.Now().Add(time.Second)) {
		t.Fatalf("Timestamp default out of range: %v", got[0].Timestamp)
	}
	if got[0].Metadata == nil || len(got[0].Metadata) != 0 {
		t.Fatalf("Metadata: want non-nil empty, got %v", got[0].Metadata)
	}
}

func TestEventService_CreateEvent_RequiresID(t *testing.T) {
	db := openEventTestDB(t)
	svc := NewEventService(db)
	err := svc.CreateEvent(t.Context(), Event{EpisodeID: "ep-1", Type: EventMessage})
	if err == nil || !strings.Contains(err.Error(), "id required") {
		t.Fatalf("want id-required error, got %v", err)
	}
}

func TestEventService_CreateEvent_RequiresEpisodeID(t *testing.T) {
	db := openEventTestDB(t)
	svc := NewEventService(db)
	err := svc.CreateEvent(t.Context(), Event{ID: "ev-1", Type: EventMessage})
	if err == nil || !strings.Contains(err.Error(), "episode_id required") {
		t.Fatalf("want episode_id-required error, got %v", err)
	}
}

func TestEventService_CreateEvent_RejectsInvalidType(t *testing.T) {
	db := openEventTestDB(t)
	seedEpisode(t, db, "ep-1")
	svc := NewEventService(db)
	err := svc.CreateEvent(t.Context(), Event{
		ID:        "ev-1",
		EpisodeID: "ep-1",
		Type:      "garbage",
	})
	if err == nil {
		t.Fatal("want error for invalid type, got nil")
	}
	if !strings.Contains(err.Error(), "invalid type") {
		t.Fatalf("err must mention invalid type, got %v", err)
	}
}

func TestEventService_CreateEvent_AllFourTypes(t *testing.T) {
	db := openEventTestDB(t)
	seedEpisode(t, db, "ep-1")
	svc := NewEventService(db)
	for i, typ := range []EventType{EventMessage, EventAction, EventObservation, EventSystem} {
		if err := svc.CreateEvent(t.Context(), Event{
			ID:        []string{"e1", "e2", "e3", "e4"}[i],
			EpisodeID: "ep-1",
			Type:      typ,
			Content:   string(typ),
		}); err != nil {
			t.Fatalf("CreateEvent %s: %v", typ, err)
		}
	}
	got, _ := svc.ListEventsByEpisode(t.Context(), "ep-1")
	if len(got) != 4 {
		t.Fatalf("want 4 events, got %d", len(got))
	}
}

func TestEventService_ListEventsByEpisode_OrdersByTimestamp(t *testing.T) {
	db := openEventTestDB(t)
	seedEpisode(t, db, "ep-1")
	svc := NewEventService(db)
	// Insert out of order — list must return sorted ASC.
	events := []Event{
		{ID: "e-c", EpisodeID: "ep-1", Type: EventMessage, Timestamp: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), Content: "third"},
		{ID: "e-a", EpisodeID: "ep-1", Type: EventMessage, Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Content: "first"},
		{ID: "e-b", EpisodeID: "ep-1", Type: EventMessage, Timestamp: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Content: "second"},
	}
	for _, e := range events {
		if err := svc.CreateEvent(t.Context(), e); err != nil {
			t.Fatalf("CreateEvent %s: %v", e.ID, err)
		}
	}
	got, _ := svc.ListEventsByEpisode(t.Context(), "ep-1")
	wantIDs := []string{"e-a", "e-b", "e-c"}
	for i, e := range got {
		if e.ID != wantIDs[i] {
			t.Errorf("position %d: want %s, got %s", i, wantIDs[i], e.ID)
		}
	}
}

func TestEventService_ListEventsByEpisode_RequiresEpisodeID(t *testing.T) {
	db := openEventTestDB(t)
	svc := NewEventService(db)
	_, err := svc.ListEventsByEpisode(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "episode_id required") {
		t.Fatalf("want episode_id-required error, got %v", err)
	}
}

func TestEventService_ListEventsByEpisode_EmptyReturnsEmptySlice(t *testing.T) {
	db := openEventTestDB(t)
	seedEpisode(t, db, "ep-empty")
	svc := NewEventService(db)
	got, err := svc.ListEventsByEpisode(t.Context(), "ep-empty")
	if err != nil {
		t.Fatalf("ListEventsByEpisode: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want 0 events, got %d", len(got))
	}
}

func TestEventService_ListEventsByType_OrdersByTimestampDesc(t *testing.T) {
	db := openEventTestDB(t)
	seedEpisode(t, db, "ep-1")
	svc := NewEventService(db)
	for i, typ := range []EventType{EventMessage, EventAction, EventMessage, EventMessage} {
		if err := svc.CreateEvent(t.Context(), Event{
			ID:        []string{"m1", "a1", "m2", "m3"}[i],
			EpisodeID: "ep-1",
			Type:      typ,
			Timestamp: time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("CreateEvent: %v", err)
		}
	}
	got, err := svc.ListEventsByType(t.Context(), EventMessage, 0)
	if err != nil {
		t.Fatalf("ListEventsByType: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("message events: want 3, got %d", len(got))
	}
	// DESC: m3 (Jan 4), m2 (Jan 3), m1 (Jan 1)
	wantIDs := []string{"m3", "m2", "m1"}
	for i, e := range got {
		if e.ID != wantIDs[i] {
			t.Errorf("position %d: want %s, got %s", i, wantIDs[i], e.ID)
		}
	}
}

func TestEventService_ListEventsByType_Limit(t *testing.T) {
	db := openEventTestDB(t)
	seedEpisode(t, db, "ep-1")
	svc := NewEventService(db)
	for i := 0; i < 5; i++ {
		if err := svc.CreateEvent(t.Context(), Event{
			ID:        []string{"e1", "e2", "e3", "e4", "e5"}[i],
			EpisodeID: "ep-1",
			Type:      EventMessage,
			Timestamp: time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("CreateEvent: %v", err)
		}
	}
	got, err := svc.ListEventsByType(t.Context(), EventMessage, 2)
	if err != nil {
		t.Fatalf("ListEventsByType: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("limit=2: want 2, got %d", len(got))
	}
}

func TestEventService_ListEventsByType_RejectsInvalidType(t *testing.T) {
	db := openEventTestDB(t)
	svc := NewEventService(db)
	_, err := svc.ListEventsByType(t.Context(), "garbage", 10)
	if err == nil || !strings.Contains(err.Error(), "invalid type") {
		t.Fatalf("want invalid-type error, got %v", err)
	}
}
