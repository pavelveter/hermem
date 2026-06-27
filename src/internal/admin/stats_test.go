package admin

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func seedStatsDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "hermem-test-stats-*.db")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	_ = f.Close()
	db, err := sql.Open("sqlite3", f.Name())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close(); os.Remove(f.Name()) })

	schema := `
	CREATE TABLE IF NOT EXISTS entities (
		id TEXT PRIMARY KEY,
		category TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		embedding BLOB,
		archived INTEGER DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS edges (
		source_id TEXT NOT NULL,
		target_id TEXT NOT NULL,
		relation_type TEXT NOT NULL,
		PRIMARY KEY (source_id, target_id, relation_type)
	);
	CREATE TABLE IF NOT EXISTS meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}

	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("e%03d", i)
		var emb interface{}
		if i < 90 {
			emb = make([]byte, 16)
		}
		archived := 0
		if i >= 95 {
			archived = 1
		}
		if _, err := db.Exec("INSERT INTO entities (id, content, embedding, archived) VALUES (?, '', ?, ?)", id, emb, archived); err != nil {
			t.Fatalf("insert entity: %v", err)
		}
	}
	for i := 0; i < 50; i++ {
		src := fmt.Sprintf("e%03d", i)
		tgt := fmt.Sprintf("e%03d", (i+1)%100)
		rt := "related_to"
		if i < 2 {
			rt = "contradicts"
		}
		if _, err := db.Exec("INSERT INTO edges (source_id, target_id, relation_type) VALUES (?, ?, ?)", src, tgt, rt); err != nil {
			t.Fatalf("insert edge: %v", err)
		}
	}
	return db
}

func TestStatsCollector_Collect(t *testing.T) {
	db := seedStatsDB(t)
	sc := NewStatsCollector(db)
	ctx := t.Context()

	stats, err := sc.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if stats.NodeCount != 100 {
		t.Errorf("NodeCount: want 100, got %d", stats.NodeCount)
	}
	if stats.EdgeCount != 50 {
		t.Errorf("EdgeCount: want 50, got %d", stats.EdgeCount)
	}
	if stats.ArchivedCount != 5 {
		t.Errorf("ArchivedCount: want 5, got %d", stats.ArchivedCount)
	}
	if stats.ContradictionCount != 2 {
		t.Errorf("ContradictionCount: want 2, got %d", stats.ContradictionCount)
	}
	wantCoverage := 0.9
	if math.Abs(stats.EmbeddingCoverage-wantCoverage) > 0.001 {
		t.Errorf("EmbeddingCoverage: want %.2f, got %.2f", wantCoverage, stats.EmbeddingCoverage)
	}
	if stats.DBSizeBytes <= 0 {
		t.Errorf("DBSizeBytes: want > 0, got %d", stats.DBSizeBytes)
	}
	if stats.CapturedAt.IsZero() {
		t.Error("CapturedAt should be set")
	}
}

func TestStatsCollector_EmptyDB(t *testing.T) {
	db, dberr := sql.Open("sqlite3", ":memory:")
	if dberr != nil {
		t.Fatalf("open: %v", dberr)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	schema := `
	CREATE TABLE IF NOT EXISTS entities (
		id TEXT PRIMARY KEY,
		category TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		embedding BLOB,
		archived INTEGER DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS edges (
		source_id TEXT NOT NULL,
		target_id TEXT NOT NULL,
		relation_type TEXT NOT NULL,
		PRIMARY KEY (source_id, target_id, relation_type)
	);
	CREATE TABLE IF NOT EXISTS meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	sc := NewStatsCollector(db)
	stats, err := sc.Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect empty: %v", err)
	}
	if stats.EmbeddingCoverage != 1.0 {
		t.Errorf("empty DB coverage: want 1.0, got %.2f", stats.EmbeddingCoverage)
	}
}

func TestStatsCollector_SingleFlight(t *testing.T) {
	db := seedStatsDB(t)
	sc := NewStatsCollector(db)
	ctx := t.Context()

	s1, err := sc.Collect(ctx)
	if err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	s2, err := sc.Collect(ctx)
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if s1.NodeCount != s2.NodeCount {
		t.Error("single-flight should return same result")
	}
}

func TestStatsCollector_CacheStaleAfter5s(t *testing.T) {
	db := seedStatsDB(t)
	sc := NewStatsCollector(db)
	ctx := t.Context()

	_, err := sc.Collect(ctx)
	if err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	sc.lastRun = time.Now().Add(-10 * time.Second)
	db.Exec("INSERT INTO entities (id, content) VALUES ('new', 'x')")
	s2, err := sc.Collect(ctx)
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if s2.NodeCount != 101 {
		t.Errorf("expected fresh 101 after cache expiry, got %d", s2.NodeCount)
	}
}
