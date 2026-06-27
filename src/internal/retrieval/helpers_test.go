package retrieval

import (
	"database/sql"
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

// openTestDB returns an in-memory SQLite for retrieval tests.
func openTestDB(tb testing.TB) *sql.DB {
	tb.Helper()
	db, err := store.MemDB()
	if err != nil {
		tb.Fatalf("memdb: %v", err)
	}
	tb.Cleanup(func() { db.Close() })
	return db
}

// seedEntity inserts a minimal entity row. (Reuses store schema.)
//
//nolint:unused // reserved for future shared test fixture
func seedEntity(tb testing.TB, db *sql.DB, id, category, content string) {
	tb.Helper()
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding) VALUES (?, ?, ?, ?)`,
		id, category, content, nil,
	); err != nil {
		tb.Fatalf("seed entity: %v", err)
	}
}

// seedEntityWithEmbedding inserts with a 3-dim embedding (matches MemDB() dim).
func seedEntityWithEmbedding(tb testing.TB, db *sql.DB, id, category, content string, emb []float32) {
	tb.Helper()
	var blob []byte
	if len(emb) > 0 {
		blob = store.EmbeddingToBytes(emb)
	}
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding) VALUES (?, ?, ?, ?)`,
		id, category, content, blob,
	); err != nil {
		tb.Fatalf("seed entity w/ emb: %v", err)
	}
}

// seedEdge inserts an edge between two entities.
func seedEdge(tb testing.TB, db *sql.DB, src, dst, relType string) {
	tb.Helper()
	const weight = 1.0
	if _, err := db.Exec(
		`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, ?, ?)`,
		src, dst, relType, weight,
	); err != nil {
		tb.Fatalf("seed edge: %v", err)
	}
}

// archive marks an entity archived = 1.
func archive(tb testing.TB, db *sql.DB, id string) {
	tb.Helper()
	if _, err := db.Exec(`UPDATE entities SET archived = 1 WHERE id = ?`, id); err != nil {
		tb.Fatalf("archive: %v", err)
	}
}
