package episodic

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// openSessionTestDB returns an in-memory SQLite with only the
// sessions table applied (no episodic FK targets needed since
// sessions is a root table).
func openSessionTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		ended_at DATETIME,
		metadata TEXT DEFAULT '{}'
	)`); err != nil {
		t.Fatalf("create sessions table: %v", err)
	}
	return db
}

func TestSessionService_CreateSession_DefaultsStartedAtAndMetadata(t *testing.T) {
	db := openSessionTestDB(t)
	svc := NewSessionService(db)

	before := time.Now()
	if err := svc.CreateSession(context.Background(), Session{ID: "s-1"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := svc.GetSession(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.StartedAt.Before(before) || got.StartedAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("StartedAt default out of range: %v", got.StartedAt)
	}
	if got.Metadata == nil || len(got.Metadata) != 0 {
		t.Fatalf("Metadata: want non-nil empty map, got %v", got.Metadata)
	}
	if got.EndedAt != nil {
		t.Fatalf("EndedAt: want nil after CreateSession, got %v", got.EndedAt)
	}
}

func TestSessionService_CreateSession_PreservesMetadata(t *testing.T) {
	db := openSessionTestDB(t)
	svc := NewSessionService(db)

	meta := map[string]any{"user": "alice", "role": float64(7)}
	if err := svc.CreateSession(context.Background(), Session{
		ID:        "s-meta",
		Metadata:  meta,
		StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, _ := svc.GetSession(context.Background(), "s-meta")
	if got.Metadata["user"] != "alice" {
		t.Errorf("metadata.user: want alice, got %v", got.Metadata["user"])
	}
	if got.Metadata["role"] != float64(7) {
		t.Errorf("metadata.role: want 7, got %v", got.Metadata["role"])
	}
}

func TestSessionService_CreateSession_RequiresID(t *testing.T) {
	db := openSessionTestDB(t)
	svc := NewSessionService(db)
	err := svc.CreateSession(context.Background(), Session{})
	if err == nil {
		t.Fatal("want error for empty id, got nil")
	}
	if !strings.Contains(err.Error(), "id required") {
		t.Fatalf("err must mention id requirement, got: %v", err)
	}
}

func TestSessionService_GetSession_NotFound(t *testing.T) {
	db := openSessionTestDB(t)
	svc := NewSessionService(db)
	_, err := svc.GetSession(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSessionService_EndSession_StampsEndedAt(t *testing.T) {
	db := openSessionTestDB(t)
	svc := NewSessionService(db)
	end := time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC)
	if err := svc.CreateSession(context.Background(), Session{ID: "s-end"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := svc.EndSession(context.Background(), "s-end", end); err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	got, _ := svc.GetSession(context.Background(), "s-end")
	if got.EndedAt == nil {
		t.Fatal("EndedAt: want non-nil after EndSession")
	}
	if !got.EndedAt.Equal(end) {
		t.Fatalf("EndedAt: want %v, got %v", end, *got.EndedAt)
	}
}

func TestSessionService_EndSession_ZeroTimeMeansNow(t *testing.T) {
	db := openSessionTestDB(t)
	svc := NewSessionService(db)
	if err := svc.CreateSession(context.Background(), Session{ID: "s-now"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	before := time.Now()
	if err := svc.EndSession(context.Background(), "s-now", time.Time{}); err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	got, _ := svc.GetSession(context.Background(), "s-now")
	if got.EndedAt == nil {
		t.Fatal("EndedAt: want non-nil after zero-time EndSession")
	}
	if got.EndedAt.Before(before) || got.EndedAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("zero-time EndSession must stamp now-ish: got %v", got.EndedAt)
	}
}

func TestSessionService_EndSession_NotFound(t *testing.T) {
	db := openSessionTestDB(t)
	svc := NewSessionService(db)
	err := svc.EndSession(context.Background(), "missing", time.Now())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSessionService_ListSessions_OrdersByStartedAtDesc(t *testing.T) {
	db := openSessionTestDB(t)
	svc := NewSessionService(db)
	for i, id := range []string{"a", "b", "c"} {
		if err := svc.CreateSession(context.Background(), Session{
			ID:        id,
			StartedAt: time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("CreateSession %s: %v", id, err)
		}
	}
	got, err := svc.ListSessions(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	wantIDs := []string{"c", "b", "a"} // DESC
	for i, s := range got {
		if s.ID != wantIDs[i] {
			t.Errorf("position %d: want %s, got %s", i, wantIDs[i], s.ID)
		}
	}
}

func TestSessionService_ListSessions_Limit(t *testing.T) {
	db := openSessionTestDB(t)
	svc := NewSessionService(db)
	for i, id := range []string{"s1", "s2", "s3", "s4", "s5"} {
		if err := svc.CreateSession(context.Background(), Session{
			ID:        id,
			StartedAt: time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("CreateSession %s: %v", id, err)
		}
	}
	got, err := svc.ListSessions(context.Background(), 3)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("limit=3: want 3, got %d", len(got))
	}
	if got[0].ID != "s5" || got[1].ID != "s4" || got[2].ID != "s3" {
		t.Fatalf("limit order: want [s5,s4,s3], got [%s,%s,%s]", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestSessionService_ListSessions_EmptyReturnsEmptySlice(t *testing.T) {
	db := openSessionTestDB(t)
	svc := NewSessionService(db)
	got, err := svc.ListSessions(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want 0 sessions, got %d", len(got))
	}
}
