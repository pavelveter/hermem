// Package episode — service-level tests for EpisodeService.
package episode

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// newEpisodeFixture wires an in-memory SQLite DB with the core schema
// and returns a Service + the DB handle. Caller closes db after test.
func newEpisodeFixture(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open mem db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS entities (
		id TEXT PRIMARY KEY,
		category TEXT DEFAULT '',
		content TEXT DEFAULT '',
		embedding BLOB,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		status TEXT DEFAULT '',
		conversation_id TEXT DEFAULT '',
		message_id TEXT DEFAULT '',
		extracted_from TEXT DEFAULT '',
		source TEXT DEFAULT '',
		source_type TEXT DEFAULT '',
		created_at DATETIME,
		confidence REAL,
		priority INTEGER DEFAULT 0,
		archived INTEGER DEFAULT 0
	)`); err != nil {
		db.Close()
		t.Fatalf("create entities table: %v", err)
	}
	return New(db), db
}

// seedProvenance inserts one row with the given provenance fields so
// ListByConversation / ListByMessage / ListBySource have data.
func seedProvenance(t *testing.T, db *sql.DB, id, conversationID, messageID, source string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, conversation_id, message_id, source, created_at)
		 VALUES (?, 'observation', ?, ?, ?, ?, datetime('now'))`,
		id, "content-"+id, conversationID, messageID, source,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestNewService_Success(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if svc == nil {
		t.Fatal("New returned nil Service")
	}
}

func TestService_ListByConversation_RequiresID(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	_, err = svc.ListByConversation(context.Background(), "", 10)
	if err == nil {
		t.Fatal("expected error for empty conversation_id")
	}
}

func TestService_ListByMessage_RequiresID(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	_, err = svc.ListByMessage(context.Background(), "", 10)
	if err == nil {
		t.Fatal("expected error for empty message_id")
	}
}

func TestService_ListBySource_RequiresSource(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	_, err = svc.ListBySource(context.Background(), "", 10)
	if err == nil {
		t.Fatal("expected error for empty source")
	}
}

func TestService_ListByConversation_ReturnsMatches(t *testing.T) {
	svc, db := newEpisodeFixture(t)
	defer db.Close()
	seedProvenance(t, db, "e1", "conv-1", "msg-a", "src-x")
	seedProvenance(t, db, "e2", "conv-1", "msg-b", "src-y")
	seedProvenance(t, db, "e3", "conv-2", "msg-c", "src-x")

	entities, err := svc.ListByConversation(context.Background(), "conv-1", 50)
	if err != nil {
		t.Fatalf("ListByConversation: %v", err)
	}
	if len(entities) != 2 {
		t.Fatalf("expected 2 entities for conv-1, got %d", len(entities))
	}
}

func TestService_ListByConversation_EmptyResultReturnsEmptySlice(t *testing.T) {
	svc, db := newEpisodeFixture(t)
	defer db.Close()

	entities, err := svc.ListByConversation(context.Background(), "no-such-conv", 10)
	if err != nil {
		t.Fatalf("ListByConversation: %v", err)
	}
	if entities == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(entities) != 0 {
		t.Fatalf("expected 0 entities, got %d", len(entities))
	}
}

func TestService_ListByMessage_ReturnsMatches(t *testing.T) {
	svc, db := newEpisodeFixture(t)
	defer db.Close()
	seedProvenance(t, db, "e1", "conv-1", "msg-a", "src-x")
	seedProvenance(t, db, "e2", "conv-2", "msg-a", "src-y")

	entities, err := svc.ListByMessage(context.Background(), "msg-a", 50)
	if err != nil {
		t.Fatalf("ListByMessage: %v", err)
	}
	if len(entities) != 2 {
		t.Fatalf("expected 2 entities for msg-a, got %d", len(entities))
	}
}

func TestService_ListBySource_ReturnsMatches(t *testing.T) {
	svc, db := newEpisodeFixture(t)
	defer db.Close()
	seedProvenance(t, db, "e1", "conv-1", "msg-a", "src-x")
	seedProvenance(t, db, "e2", "conv-2", "msg-b", "src-x")

	entities, err := svc.ListBySource(context.Background(), "src-x", 50)
	if err != nil {
		t.Fatalf("ListBySource: %v", err)
	}
	if len(entities) != 2 {
		t.Fatalf("expected 2 entities for src-x, got %d", len(entities))
	}
}

func TestService_ListByConversation_RespectsLimit(t *testing.T) {
	svc, db := newEpisodeFixture(t)
	defer db.Close()
	for i := 0; i < 5; i++ {
		id := "e" + string(rune('a'+i))
		seedProvenance(t, db, id, "conv-limit", "msg-"+id, "src")
	}
	entities, err := svc.ListByConversation(context.Background(), "conv-limit", 2)
	if err != nil {
		t.Fatalf("ListByConversation: %v", err)
	}
	if len(entities) != 2 {
		t.Fatalf("expected 2 entities with limit=2, got %d", len(entities))
	}
}
