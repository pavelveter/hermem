package store

import (
	"database/sql"
	"testing"
	"time"
)

// openTestDB returns an in-memory SQLite database with the full hermem schema.
// Uses MemDBRandom so concurrent tests under -race don't share the global
// `:memory:` cache and corrupt each other's fixtures.
//
// NB: SQLite reverts PRAGMA journal_mode=WAL on shared in-memory databases
// (see MemDBRandom's WARNING). Tests that need to assert WAL via
// verifyPragmaOrder must NOT use this helper — open a t.TempDir-backed
// DB directly, e.g. InitDB(filepath.Join(t.TempDir(), "name.db"), <yourDim=3>).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := MemDBRandom()
	if err != nil {
		t.Fatalf("memdb random: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedEntity inserts a minimal entity row. Returns ID for chain tests.
func seedEntity(t *testing.T, db *sql.DB, id, category, content string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO entities (id, category, content) VALUES (?, ?, ?)`,
		id, category, content,
	)
	if err != nil {
		t.Fatalf("seed entity %s: %v", id, err)
	}
}

// seedEntityFull inserts an entity with status, updated_at and optional embedding.
func seedEntityFull(t *testing.T, db *sql.DB, id, category, content, status string, updatedAt time.Time, embedding []float32) {
	t.Helper()
	var blob []byte
	if len(embedding) > 0 {
		blob = EmbeddingToBytes(embedding)
	}
	var statusVal interface{}
	if status != "" {
		statusVal = status
	}
	var tVal interface{}
	if !updatedAt.IsZero() {
		tVal = updatedAt
	}
	_, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding, status, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, category, content, blob, statusVal, tVal,
	)
	if err != nil {
		t.Fatalf("seed entity full %s: %v", id, err)
	}
}

// seedEdge inserts a directed edge of a given relation type with optional weight.
func seedEdge(t *testing.T, db *sql.DB, src, dst, relType string, weight float32) {
	t.Helper()
	var w interface{}
	if weight != 0 {
		w = weight
	}
	_, err := db.Exec(
		`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, ?, ?)`,
		src, dst, relType, w,
	)
	if err != nil {
		t.Fatalf("seed edge %s -> %s: %v", src, dst, err)
	}
}
