package ingestion

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

func TestAdaptiveLinkThreshold_NilDBReturnsBase(t *testing.T) {
	got := AdaptiveLinkThreshold(nil, "x", 0.85)
	if got != 0.85 {
		t.Fatalf("nil db: want 0.85, got %v", got)
	}
}

func TestAdaptiveLinkThreshold_EmptyIDReturnsBase(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got := AdaptiveLinkThreshold(db, "", 0.85)
	if got != 0.85 {
		t.Fatalf("empty id: want 0.85, got %v", got)
	}
}

func TestAdaptiveLinkThreshold_NoNeighborsReturnsBase(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.Exec(`INSERT INTO entities (id, category, content) VALUES (?, ?, ?)`, "orphan", "world", "alone")
	got := AdaptiveLinkThreshold(db, "orphan", 0.85)
	if got != 0.85 {
		t.Fatalf("no neighbors: want 0.85, got %v", got)
	}
}

func TestAdaptiveLinkThreshold_DenseNeighborhoodIncreasesThreshold(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Create a hub entity with many high-degree neighbors.
	db.Exec(`INSERT INTO entities (id, category, content, degree) VALUES (?, ?, ?, ?)`, "hub", "world", "hub", 20)
	for i := 0; i < 5; i++ {
		id := "n" + string(rune('0'+i))
		db.Exec(`INSERT INTO entities (id, category, content, degree) VALUES (?, ?, ?, ?)`, id, "world", "neighbor", 15)
		db.Exec(`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, ?, 1.0)`, "hub", id, "related_to")
	}
	got := AdaptiveLinkThreshold(db, "hub", 0.85)
	if got <= 0.85 {
		t.Fatalf("dense neighborhood should increase threshold: got %v, want > 0.85", got)
	}
	if got > 1.0 {
		t.Fatalf("threshold capped at 1.0: got %v", got)
	}
}
