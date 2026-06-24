package retrieval

import (
	"database/sql"
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

// openTestDB returns an in-memory SQLite for retrieval tests.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedEntity inserts a minimal entity row. (Reuses store schema.)
func seedEntity(t *testing.T, db *sql.DB, id, category, content string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding) VALUES (?, ?, ?, ?)`,
		id, category, content, nil,
	); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
}

// seedEntityWithEmbedding inserts with a 3-dim embedding (matches MemDB() dim).
func seedEntityWithEmbedding(t *testing.T, db *sql.DB, id, category, content string, emb []float32) {
	t.Helper()
	var blob []byte
	if len(emb) > 0 {
		blob = store.EmbeddingToBytes(emb)
	}
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding) VALUES (?, ?, ?, ?)`,
		id, category, content, blob,
	); err != nil {
		t.Fatalf("seed entity w/ emb: %v", err)
	}
}

// seedEdge inserts an edge between two entities.
func seedEdge(t *testing.T, db *sql.DB, src, dst, relType string) {
	t.Helper()
	const weight = 1.0
	if _, err := db.Exec(
		`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, ?, ?)`,
		src, dst, relType, weight,
	); err != nil {
		t.Fatalf("seed edge: %v", err)
	}
}

// archive marks an entity archived = 1.
func archive(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE entities SET archived = 1 WHERE id = ?`, id); err != nil {
		t.Fatalf("archive: %v", err)
	}
}
