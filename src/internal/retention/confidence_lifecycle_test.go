package retention

import (
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/store"
)

func TestConfidenceLifecycle_DisabledNoOp(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cl := NewConfidenceLifecycle(db)
	cfg := DefaultConfidenceLifecycleConfig() // Enabled=false
	rep, err := cl.RunOnce(t.Context(), cfg)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.Archived != 0 {
		t.Fatalf("disabled lifecycle should archive 0, got %d", rep.Archived)
	}
}

func TestConfidenceLifecycle_ArchivesExpiredLowConfidence(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert a low-confidence entity with an old updated_at.
	old := time.Now().Add(-60 * 24 * time.Hour)
	db.Exec(`INSERT INTO entities (id, category, content, confidence, updated_at, archived) VALUES (?, ?, ?, ?, ?, 0)`,
		"low1", "world", "low confidence", 0.3, old)

	// Insert a high-confidence entity (should NOT be archived).
	db.Exec(`INSERT INTO entities (id, category, content, confidence, updated_at, archived) VALUES (?, ?, ?, ?, ?, 0)`,
		"high1", "world", "high confidence", 1.0, old)

	// Insert a recent low-confidence entity (should NOT be archived — not expired yet).
	recent := time.Now().Add(-1 * time.Hour)
	db.Exec(`INSERT INTO entities (id, category, content, confidence, updated_at, archived) VALUES (?, ?, ?, ?, ?, 0)`,
		"low2", "world", "recent low", 0.4, recent)

	cl := NewConfidenceLifecycle(db)
	cfg := ConfidenceLifecycleConfig{
		Enabled:             true,
		ConfidenceThreshold: 0.7,
		TTL:                 30 * 24 * time.Hour,
		BatchSize:           100,
	}

	rep, err := cl.RunOnce(t.Context(), cfg)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.Archived != 1 {
		t.Fatalf("want 1 archived, got %d", rep.Archived)
	}

	// Verify only low1 is archived.
	var archived int
	db.QueryRow(`SELECT COUNT(*) FROM entities WHERE archived = 1`).Scan(&archived)
	if archived != 1 {
		t.Fatalf("want 1 archived entity, got %d", archived)
	}
	var low1Archived int
	db.QueryRow(`SELECT archived FROM entities WHERE id = 'low1'`).Scan(&low1Archived)
	if low1Archived != 1 {
		t.Fatal("low1 should be archived")
	}
}

func TestConfidenceLifecycle_SkipsZeroConfidence(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Entities with confidence=0 are treated as "not set" and should not be archived.
	old := time.Now().Add(-60 * 24 * time.Hour)
	db.Exec(`INSERT INTO entities (id, category, content, confidence, updated_at, archived) VALUES (?, ?, ?, ?, ?, 0)`,
		"zero", "world", "zero conf", 0, old)

	cl := NewConfidenceLifecycle(db)
	cfg := ConfidenceLifecycleConfig{
		Enabled:             true,
		ConfidenceThreshold: 0.7,
		TTL:                 30 * 24 * time.Hour,
		BatchSize:           100,
	}
	rep, err := cl.RunOnce(t.Context(), cfg)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.Archived != 0 {
		t.Fatalf("zero confidence should not be archived, got %d", rep.Archived)
	}
}
