package admin

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync/atomic"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

type mockEmbedder struct {
	fixedVec []float32
	err      error
}

func (m *mockEmbedder) Embed(_ context.Context, content string) ([]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.fixedVec, nil
}

func (m *mockEmbedder) Ping(_ context.Context) error {
	return nil
}

type mockVectorIndex struct {
	storeCallCount  int64
	removeCallCount int64
	storeErr        error
	removeErr       error
}

func (m *mockVectorIndex) Store(_ context.Context, id string, vec []float32) error {
	atomic.AddInt64(&m.storeCallCount, 1)
	return m.storeErr
}

func (m *mockVectorIndex) Remove(_ context.Context, ids []string) error {
	atomic.AddInt64(&m.removeCallCount, 1)
	return m.removeErr
}

func seedRebuildDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "hermem-test-rebuild-*.db")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	_ = f.Close() //nolint:errcheck // test teardown
	db, err := sql.Open("sqlite3", f.Name())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close(); os.Remove(f.Name()) })

	db.Exec(`CREATE TABLE entities (
		id TEXT PRIMARY KEY, category TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '', embedding BLOB,
		archived INTEGER DEFAULT 0, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	db.Exec(`INSERT INTO entities (id, category, content) VALUES ('e1', 'fact', 'first content')`)
	db.Exec(`INSERT INTO entities (id, category, content) VALUES ('e2', 'fact', 'second content')`)
	db.Exec(`INSERT INTO entities (id, category, content, archived) VALUES ('e3', 'memory', 'archived content', 1)`)
	return db
}

func TestRebuildIndex_DryRun(t *testing.T) {
	db := seedRebuildDB(t)
	em := &mockEmbedder{fixedVec: []float32{0.1, 0.2}}
	vi := &mockVectorIndex{}
	ri := NewRebuildIndex(db, vi, em)

	var logs []string
	ri.OnLog(func(msg string) {
		logs = append(logs, msg)
	})

	report, err := ri.Run(t.Context(), RebuildOpts{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Processed != 2 {
		t.Errorf("expected 2 processed (dry run only non-archived), got %d", report.Processed)
	}
	if report.Reembedded != 0 {
		t.Errorf("expected 0 reembedded in dry run, got %d", report.Reembedded)
	}
	if vi.storeCallCount != 0 {
		t.Errorf("expected 0 store calls in dry run, got %d", vi.storeCallCount)
	}
	if len(logs) == 0 {
		t.Error("expected OnLog to be called")
	}
}

func TestRebuildIndex_Run(t *testing.T) {
	db := seedRebuildDB(t)
	em := &mockEmbedder{fixedVec: []float32{0.1, 0.2}}
	vi := &mockVectorIndex{}
	ri := NewRebuildIndex(db, vi, em)

	report, err := ri.Run(t.Context(), RebuildOpts{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Processed != 2 {
		t.Errorf("expected 2 processed, got %d", report.Processed)
	}
	if report.Reembedded != 2 {
		t.Errorf("expected 2 reembedded, got %d", report.Reembedded)
	}
	if vi.removeCallCount != 2 {
		t.Errorf("expected 2 remove calls, got %d", vi.removeCallCount)
	}
	if vi.storeCallCount != 2 {
		t.Errorf("expected 2 store calls, got %d", vi.storeCallCount)
	}
}

func TestRebuildIndex_FailedEntities(t *testing.T) {
	db := seedRebuildDB(t)
	em := &mockEmbedder{err: errors.New("embed failed")}
	vi := &mockVectorIndex{}
	ri := NewRebuildIndex(db, vi, em)

	report, err := ri.Run(t.Context(), RebuildOpts{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Processed != 2 {
		t.Errorf("expected 2 processed, got %d", report.Processed)
	}
	if report.Failed != 2 {
		t.Errorf("expected 2 failed, got %d", report.Failed)
	}
	if vi.storeCallCount != 0 {
		t.Errorf("expected 0 store calls on failure, got %d", vi.storeCallCount)
	}
}

func TestRebuildIndex_EmptyFilter(t *testing.T) {
	db := seedRebuildDB(t)
	em := &mockEmbedder{fixedVec: []float32{0.1, 0.2}}
	vi := &mockVectorIndex{}
	ri := NewRebuildIndex(db, vi, em)

	report, err := ri.Run(t.Context(), RebuildOpts{Category: "nonexistent"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Processed != 0 {
		t.Errorf("expected 0 processed for nonexistent category, got %d", report.Processed)
	}
}

func TestRebuildIndex_OnlyArchived(t *testing.T) {
	db := seedRebuildDB(t)
	em := &mockEmbedder{fixedVec: []float32{0.1, 0.2}}
	vi := &mockVectorIndex{}
	ri := NewRebuildIndex(db, vi, em)

	report, err := ri.Run(t.Context(), RebuildOpts{OnlyArchived: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Processed != 1 {
		t.Errorf("expected 1 processed (only archived), got %d", report.Processed)
	}
}

func TestRebuildIndex_EmptyDB(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	db.Exec("CREATE TABLE entities (id TEXT PRIMARY KEY, category TEXT, content TEXT, embedding BLOB, archived INTEGER DEFAULT 0, updated_at DATETIME)")

	em := &mockEmbedder{fixedVec: []float32{0.1, 0.2}}
	vi := &mockVectorIndex{}
	ri := NewRebuildIndex(db, vi, em)

	report, err := ri.Run(t.Context(), RebuildOpts{})
	if err != nil {
		t.Fatalf("Run empty: %v", err)
	}
	if report.Processed != 0 {
		t.Errorf("expected 0 for empty DB, got %d", report.Processed)
	}
}
