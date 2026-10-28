package vector

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/pkg/domain"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/testutil"
)

// TestWarm_MakesPersistedEmbeddingsSearchable reproduces the failure this
// warm-up exists for: a fresh process builds an empty in-memory index, so
// search returns nothing even though `entities` holds the vectors.
func TestWarm_MakesPersistedEmbeddingsSearchable(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	schema := domain.DefaultSchemaConfig(false)
	vec := []float32{0.1, 0.2, 0.3}
	if err := store.StoreEntityWithEmbedding(ctx, db, NewInMemoryVectorStore(db, 0), schema, domain.Entity{
		ID: "seeded", Category: "world", Content: "seeded content", Embedding: vec,
	}); err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	// Fresh process: a new store knows nothing about the persisted row.
	vi := NewInMemoryVectorStore(db, 0)
	if results, err := SearchByVector(ctx, db, vi, vec, 5); err != nil || len(results) != 0 {
		t.Fatalf("pre-warm search: want 0 results, nil error; got %d results, %v", len(results), err)
	}

	loaded, skipped, err := Warm(ctx, db, vi, 3)
	if err != nil {
		t.Fatalf("Warm: %v", err)
	}
	if loaded != 1 || skipped != 0 {
		t.Fatalf("want loaded=1 skipped=0, got loaded=%d skipped=%d", loaded, skipped)
	}

	results, err := SearchByVector(ctx, db, vi, vec, 5)
	if err != nil {
		t.Fatalf("post-warm search: %v", err)
	}
	if len(results) != 1 || results[0].Entity.ID != "seeded" {
		t.Fatalf("post-warm search: want the seeded entity, got %+v", results)
	}
}

// TestWarm_SkipsUnusableRows — one corrupt blob or a dimension drift must not
// abort the warm-up (that would leave the whole knowledge base unsearchable).
func TestWarm_SkipsUnusableRows(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	schema := domain.DefaultSchemaConfig(false)
	vi0 := NewInMemoryVectorStore(db, 0)

	if err := store.StoreEntityWithEmbedding(ctx, db, vi0, schema, domain.Entity{
		ID: "good", Category: "world", Content: "good", Embedding: []float32{0.1, 0.2, 0.3},
	}); err != nil {
		t.Fatalf("seed good entity: %v", err)
	}
	// Wrong dimension (2 floats where the config says 3): direct insert,
	// because the vector store itself refuses dimension drift.
	if _, err := db.ExecContext(ctx, `INSERT INTO entities (id, category, content, embedding) VALUES (?, ?, ?, ?)`,
		"drifted", "world", "drifted", store.EmbeddingToBytes([]float32{0.1, 0.2})); err != nil {
		t.Fatalf("seed drifted entity: %v", err)
	}
	// Garbage blob that is not a whole number of float32s.
	if _, err := db.ExecContext(ctx, `INSERT INTO entities (id, category, content, embedding) VALUES (?, ?, ?, ?)`,
		"corrupt", "world", "corrupt", []byte{1, 2, 3}); err != nil {
		t.Fatalf("seed corrupt entity: %v", err)
	}

	vi := NewInMemoryVectorStore(db, 0)
	loaded, skipped, err := Warm(ctx, db, vi, 3)
	if err != nil {
		t.Fatalf("Warm: %v", err)
	}
	if loaded != 1 || skipped != 2 {
		t.Fatalf("want loaded=1 skipped=2, got loaded=%d skipped=%d", loaded, skipped)
	}
}

// TestWarm_RejectsNonPositiveDim — a missing config dimension would silently
// skip every row, which is exactly the silent emptiness Warm exists to fix.
func TestWarm_RejectsNonPositiveDim(t *testing.T) {
	db := testutil.OpenTestDB(t)
	if _, _, err := Warm(context.Background(), db, NewInMemoryVectorStore(db, 0), 0); err == nil {
		t.Fatal("want error for dim=0, got nil")
	}
}
