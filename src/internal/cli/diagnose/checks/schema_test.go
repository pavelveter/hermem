package checks

import (
	"database/sql"
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("MemDBRandom: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCheckSchema_CleanDB(t *testing.T) {
	db := openDB(t)
	r, err := CheckSchema(db)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if !r.ForeignKeysOK {
		t.Error("expected ForeignKeysOK=true on clean DB")
	}
	if r.OrphanEdges != 0 {
		t.Errorf("expected OrphanEdges=0, got %d", r.OrphanEdges)
	}
	if !r.IntegrityOK {
		t.Error("expected IntegrityOK=true on clean DB")
	}
}

func TestCheckSchema_OrphanEdges(t *testing.T) {
	db := openDB(t)
	// Insert an edge referencing non-existent entities — FK constraints
	// would block this, so we use PRAGMA foreign_keys = OFF temporarily.
	_, err := db.Exec("PRAGMA foreign_keys = OFF")
	if err != nil {
		t.Fatalf("disable FK: %v", err)
	}
	_, err = db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('ghost', 'phantom', 'uses')`)
	if err != nil {
		t.Fatalf("insert orphan edge: %v", err)
	}
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	if err != nil {
		t.Fatalf("enable FK: %v", err)
	}

	r, err := CheckSchema(db)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if r.OrphanEdges != 1 {
		t.Errorf("expected OrphanEdges=1, got %d", r.OrphanEdges)
	}
	if r.ForeignKeysOK {
		t.Error("expected ForeignKeysOK=false with orphan edges")
	}
}
