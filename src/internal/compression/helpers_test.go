package compression

import (
	"database/sql"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/store"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedEntity(t *testing.T, db *sql.DB, id, category, content string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO entities (id, category, content) VALUES (?, ?, ?)`, id, category, content)
	if err != nil {
		t.Fatalf("seed entity %s: %v", id, err)
	}
}

func seedEntityFull(t *testing.T, db *sql.DB, id, category, content string, status string, updatedAt time.Time, embedding []float32) {
	t.Helper()
	var blob []byte
	if len(embedding) > 0 {
		blob = store.EmbeddingToBytes(embedding)
	}
	var s interface{}
	if status != "" {
		s = status
	}
	var tVal interface{}
	if !updatedAt.IsZero() {
		tVal = updatedAt
	}
	_, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding, status, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, category, content, blob, s, tVal,
	)
	if err != nil {
		t.Fatalf("seed entity full %s: %v", id, err)
	}
}

var zeroTime time.Time
