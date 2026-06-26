package checks

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

func TestCheckMemory_CleanDB(t *testing.T) {
	db := openDB(t)
	r, err := CheckMemory(db)
	if err != nil {
		t.Fatalf("CheckMemory: %v", err)
	}
	if r.TotalEntities != 0 {
		t.Errorf("expected TotalEntities=0, got %d", r.TotalEntities)
	}
	if r.EntitiesWithEmbedding != 0 {
		t.Errorf("expected EntitiesWithEmbedding=0, got %d", r.EntitiesWithEmbedding)
	}
	if len(r.DensityByCategory) != 0 {
		t.Errorf("expected empty DensityByCategory, got %v", r.DensityByCategory)
	}
}

func TestCheckMemory_WithEntities(t *testing.T) {
	db := openDB(t)
	_, err := db.Exec(`INSERT INTO entities (id, category, content, embedding) VALUES (?, ?, ?, ?)`,
		"e1", "world", "fact one", store.EmbeddingToBytes([]float32{0.1, 0.2, 0.3}))
	if err != nil {
		t.Fatalf("insert e1: %v", err)
	}
	_, err = db.Exec(`INSERT INTO entities (id, category, content) VALUES (?, ?, ?)`,
		"e2", "observation", "fact two")
	if err != nil {
		t.Fatalf("insert e2: %v", err)
	}
	_, err = db.Exec(`INSERT INTO entities (id, category, content) VALUES (?, ?, ?)`,
		"e3", "observation", "fact three")
	if err != nil {
		t.Fatalf("insert e3: %v", err)
	}

	r, err := CheckMemory(db)
	if err != nil {
		t.Fatalf("CheckMemory: %v", err)
	}
	if r.TotalEntities != 3 {
		t.Errorf("expected TotalEntities=3, got %d", r.TotalEntities)
	}
	if r.EntitiesWithEmbedding != 1 {
		t.Errorf("expected EntitiesWithEmbedding=1, got %d", r.EntitiesWithEmbedding)
	}
	if r.EmbeddingDensity != 33.33333333333333 {
		t.Errorf("expected density ~33.3%%, got %f", r.EmbeddingDensity)
	}
	// Check category breakdown.
	if pct, ok := r.DensityByCategory["world"]; !ok || pct != 100 {
		t.Errorf("expected world density=100%%, got %f", pct)
	}
	if pct, ok := r.DensityByCategory["observation"]; !ok || pct != 0 {
		t.Errorf("expected observation density=0%%, got %f", pct)
	}
}

func TestCheckMemory_BeliefsTable(t *testing.T) {
	db := openDB(t)
	// beliefs table exists via migrations; insert one row.
	_, err := db.Exec(`INSERT INTO beliefs (content, confidence, status) VALUES (?, ?, ?)`,
		"test belief", 0.8, "Active")
	if err != nil {
		t.Fatalf("insert belief: %v", err)
	}
	_, err = db.Exec(`INSERT INTO beliefs (content, confidence, status) VALUES (?, ?, ?)`,
		"superseded belief", 0.5, "Superseded")
	if err != nil {
		t.Fatalf("insert superseded: %v", err)
	}

	r, err := CheckMemory(db)
	if err != nil {
		t.Fatalf("CheckMemory: %v", err)
	}
	if r.BeliefCounts["Active"] != 1 {
		t.Errorf("expected Active=1, got %d", r.BeliefCounts["Active"])
	}
	if r.BeliefCounts["Superseded"] != 1 {
		t.Errorf("expected Superseded=1, got %d", r.BeliefCounts["Superseded"])
	}
}
