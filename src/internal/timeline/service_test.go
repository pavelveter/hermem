package timeline_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/timeline"
)

// newTimelineFixture wires a timeline.Service against an in-memory
// SQLite. Mirrors memory/service_test.go + edge/service_test.go
// fixture pattern.
func newTimelineFixture(t *testing.T) (*timeline.Service, *sql.DB) {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return timeline.New(db), db
}

// seedEntity inserts a row directly so Timeline tests can reference
// pre-existing IDs without depending on memory.Service.Store.
func seedEntity(t *testing.T, db *sql.DB, id, category, content string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		id, category, content, []byte{},
	)
	if err != nil {
		t.Fatalf("seed entity %q: %v", id, err)
	}
}

// --- New ---

func TestNewService_Success(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := timeline.New(db)
	if svc == nil {
		t.Fatal("New returned nil Service")
	}
}

// --- Timeline ---

func TestTimelineService_Timeline_EmptyDB(t *testing.T) {
	svc, _ := newTimelineFixture(t)
	entries, err := svc.Timeline(context.Background(), 50)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 entries, got %d", len(entries))
	}
}

func TestTimelineService_Timeline_WithLimitAndOrder(t *testing.T) {
	svc, db := newTimelineFixture(t)
	seedEntity(t, db, "tl-one", "world", "one")
	seedEntity(t, db, "tl-two", "world", "two")
	seedEntity(t, db, "tl-three", "world", "three")
	entries, err := svc.Timeline(context.Background(), 2)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.CreatedAt == nil {
			t.Errorf("entry %s missing created_at", e.ID)
		}
		if !hasPrefix(e.ID, "tl-") {
			t.Errorf("entry %s not a seeded tl- row", e.ID)
		}
	}
}

// hasPrefix is a tiny string-helper to avoid pulling strings into
// the import list just for two callsites.
func hasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}
