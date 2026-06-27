package admin

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func seedIntegrityCheckDB(t *testing.T, missingEmbeddings, danglingEdges int, archivedWithEmbedding bool) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "hermem-test-int-*.db")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	_ = f.Close() //nolint:errcheck // test teardown
	db, err := sql.Open("sqlite3", f.Name())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close(); os.Remove(f.Name()) })

	db.Exec(`CREATE TABLE IF NOT EXISTS entities (
		id TEXT PRIMARY KEY, category TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '', embedding BLOB,
		archived INTEGER DEFAULT 0)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS edges (
		source_id TEXT NOT NULL, target_id TEXT NOT NULL,
		relation_type TEXT NOT NULL,
		PRIMARY KEY (source_id, target_id, relation_type))`)

	// Normal entities
	for i := 0; i < 100; i++ {
		id := "e" + itoa(i)
		emb := make([]byte, 16)
		db.Exec("INSERT OR IGNORE INTO entities (id, category, content, embedding) VALUES (?, 'test', 'x', ?)", id, emb)
		// Correlating edge
		tgt := "e" + itoa((i+1)%100)
		db.Exec("INSERT OR IGNORE INTO edges (source_id, target_id, relation_type) VALUES (?, ?, 'related_to')", id, tgt)
	}

	// Missing embeddings
	for i := 0; i < missingEmbeddings; i++ {
		id := "missing" + itoa(i)
		db.Exec("INSERT INTO entities (id, category, content) VALUES (?, 'test', 'x')", id)
	}

	// Dangling edges
	for i := 0; i < danglingEdges; i++ {
		db.Exec("INSERT INTO edges (source_id, target_id, relation_type) VALUES (?, ?, 'dangling')", "ghost"+itoa(i), "e000")
	}

	// Archived with embedding
	if archivedWithEmbedding {
		db.Exec("INSERT INTO entities (id, category, content, embedding, archived) VALUES ('archived-with-emb', 'test', 'x', ?, 1)", make([]byte, 16))
	}

	return db
}

func TestIntegrityChecker_CleanDB(t *testing.T) {
	db := seedIntegrityCheckDB(t, 0, 0, false)
	ic := NewIntegrityChecker(db)
	report, err := ic.Check(t.Context())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.OK {
		t.Errorf("expected OK for clean DB, got %d issues", len(report.Issues))
	}
}

func TestIntegrityChecker_MissingEmbeddings(t *testing.T) {
	db := seedIntegrityCheckDB(t, 1, 0, false)
	ic := NewIntegrityChecker(db)
	report, err := ic.Check(t.Context())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Code == "MISSING_EMBEDDING" {
			found = true
			if iss.Level != IssueWarning {
				t.Errorf("single missing embedding should be warning, got %s", iss.Level)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected MISSING_EMBEDDING issue, got: %+v", report.Issues)
	}
}

func TestIntegrityChecker_DanglingEdge(t *testing.T) {
	db := seedIntegrityCheckDB(t, 0, 1, false)
	ic := NewIntegrityChecker(db)
	report, err := ic.Check(t.Context())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Code == "DANGLING_EDGE" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected DANGLING_EDGE issue, got: %+v", report.Issues)
	}
}

func TestIntegrityChecker_ArchivedWithEmbedding(t *testing.T) {
	db := seedIntegrityCheckDB(t, 0, 0, true)
	ic := NewIntegrityChecker(db)
	report, err := ic.Check(t.Context())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Code == "ARCHIVE_CONSISTENCY" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ARCHIVE_CONSISTENCY issue, got: %+v", report.Issues)
	}
}

func TestIntegrityChecker_ManyMissingEmbeddingsCritical(t *testing.T) {
	db := seedIntegrityCheckDB(t, 10, 0, false)
	ic := NewIntegrityChecker(db)
	report, err := ic.Check(t.Context())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.CriticalExist() {
		t.Error("expected critical when 10+ entities missing embeddings")
	}
}

func itoa(i int) string {
	return fmt.Sprintf("%03d", i)
}
