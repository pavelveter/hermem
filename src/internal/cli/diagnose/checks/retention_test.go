package checks

import (
	"testing"
)

func TestCheckRetention_CleanDB(t *testing.T) {
	db := openDB(t)
	r, err := CheckRetention(db)
	if err != nil {
		t.Fatalf("CheckRetention: %v", err)
	}
	if r.TotalEntities != 0 {
		t.Errorf("expected TotalEntities=0, got %d", r.TotalEntities)
	}
	if r.ArchivedEntities != 0 {
		t.Errorf("expected ArchivedEntities=0, got %d", r.ArchivedEntities)
	}
}

func TestCheckRetention_WithArchived(t *testing.T) {
	db := openDB(t)
	_, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES ('a1', 'world', 'active')`)
	if err != nil {
		t.Fatalf("insert a1: %v", err)
	}
	_, err = db.Exec(`INSERT INTO entities (id, category, content) VALUES ('a2', 'world', 'active 2')`)
	if err != nil {
		t.Fatalf("insert a2: %v", err)
	}
	_, err = db.Exec(`INSERT INTO entities (id, category, content, archived) VALUES ('a3', 'observation', 'archived', 1)`)
	if err != nil {
		t.Fatalf("insert a3: %v", err)
	}

	r, err := CheckRetention(db)
	if err != nil {
		t.Fatalf("CheckRetention: %v", err)
	}
	if r.TotalEntities != 3 {
		t.Errorf("expected TotalEntities=3, got %d", r.TotalEntities)
	}
	if r.ArchivedEntities != 1 {
		t.Errorf("expected ArchivedEntities=1, got %d", r.ArchivedEntities)
	}
	if r.ArchivedPct != 33.33333333333333 {
		t.Errorf("expected ArchivedPct~33.3, got %f", r.ArchivedPct)
	}
}
