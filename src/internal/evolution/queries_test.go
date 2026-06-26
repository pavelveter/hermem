package evolution

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/store"
)

func TestGetSupersededBy_Active(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence, status) VALUES (1, 'active', 1.0, 'Active')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	id, err := GetSupersededBy(ctx, db, 1)
	if err != nil {
		t.Fatalf("GetSupersededBy: %v", err)
	}
	if id != 0 {
		t.Errorf("expected 0 for active belief, got %d", id)
	}
}

func TestGetSupersededBy_Superseded(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence) VALUES (1, 'old', 0.5), (2, 'new', 0.8)`); err != nil {
		t.Fatalf("insert beliefs: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE beliefs SET status='Superseded', superseded_by=2 WHERE id=1`); err != nil {
		t.Fatalf("set superseded: %v", err)
	}

	id, err := GetSupersededBy(ctx, db, 1)
	if err != nil {
		t.Fatalf("GetSupersededBy: %v", err)
	}
	if id != 2 {
		t.Errorf("expected 2, got %d", id)
	}
}

func TestGetSupersededBy_NotFound(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	id, err := GetSupersededBy(ctx, db, 999)
	if err != nil {
		t.Fatalf("GetSupersededBy: %v", err)
	}
	if id != 0 {
		t.Errorf("expected 0 for not found, got %d", id)
	}
}

func TestStateAt_BeforeHistory(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence) VALUES (1, 'test', 1.0)`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := RecordHistory(ctx, db, 1, 0.8, "Active", "updated"); err != nil {
		t.Fatalf("RecordHistory: %v", err)
	}

	before := time.Now().UTC().Add(-1 * time.Hour)
	h, err := StateAt(ctx, db, 1, before)
	if err != nil {
		t.Fatalf("StateAt: %v", err)
	}
	if h != nil {
		t.Errorf("expected nil before first history entry, got entry id=%d", h.ID)
	}
}

func TestStateAt_AfterHistory(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence) VALUES (1, 'test', 1.0)`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := RecordHistory(ctx, db, 1, 0.8, "Active", "updated"); err != nil {
		t.Fatalf("RecordHistory: %v", err)
	}

	after := time.Now().UTC().Add(1 * time.Hour)
	h, err := StateAt(ctx, db, 1, after)
	if err != nil {
		t.Fatalf("StateAt: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil entry")
	}
	if h.Confidence != 0.8 {
		t.Errorf("expected confidence 0.8, got %f", h.Confidence)
	}
}

func openDBBench(b *testing.B) *sql.DB {
	b.Helper()
	db, err := store.MemDBRandom()
	if err != nil {
		b.Fatalf("MemDBRandom: %v", err)
	}
	b.Cleanup(func() { db.Close() })
	return db
}

func BenchmarkStateAt(b *testing.B) {
	db := openDBBench(b)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `INSERT INTO beliefs (id, content, confidence) VALUES (1, 'bench', 1.0)`); err != nil {
		b.Fatalf("insert: %v", err)
	}
	for i := 0; i < 100; i++ {
		if err := RecordHistory(ctx, db, 1, 0.5, "Active", "bench"); err != nil {
			b.Fatalf("RecordHistory: %v", err)
		}
	}

	after := time.Now().UTC().Add(1 * time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StateAt(ctx, db, 1, after)
	}
}
