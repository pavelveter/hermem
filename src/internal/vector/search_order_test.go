package vector

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/testutil"
)

// TestSearchByVector_PreservesSimilarityOrder guards the documented contract
// of SearchByVector: results come back most-similar-first. The SQL hydration
// step (`WHERE id IN (...)`) returns rows in storage order, so without an
// explicit reorder the ranking produced by the vector store is lost.
func TestSearchByVector_PreservesSimilarityOrder(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	vi := NewInMemoryVectorStore(db, 0)

	vectors := []struct {
		id  string
		vec []float32
	}{
		{"far", []float32{0, 1}},
		{"near", []float32{1, 0}},
		{"mid", []float32{1, 1}},
	}
	// Insert in non-ranking order so storage order cannot accidentally match.
	for _, v := range vectors {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO entities (id, category, content, embedding) VALUES (?, 'world', ?, ?)`,
			v.id, v.id, store.EmbeddingToBytes(v.vec)); err != nil {
			t.Fatalf("seed %s: %v", v.id, err)
		}
		if err := vi.Upsert(ctx, []spi.VectorRecord{{Namespace: spi.DefaultNamespace, ID: v.id, Vector: v.vec}}); err != nil {
			t.Fatalf("upsert %s: %v", v.id, err)
		}
	}

	results, err := SearchByVector(ctx, db, vi, []float32{1, 0}, 3)
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	want := []string{"near", "mid", "far"}
	if len(results) != len(want) {
		t.Fatalf("want %d results, got %d: %+v", len(want), len(results), results)
	}
	for i, id := range want {
		if results[i].Entity.ID != id {
			t.Fatalf("position %d: want %q, got %q (all: %+v)", i, id, results[i].Entity.ID, results)
		}
	}
	for i := 1; i < len(results); i++ {
		if results[i-1].Similarity < results[i].Similarity {
			t.Fatalf("similarity not descending at %d: %v < %v", i, results[i-1].Similarity, results[i].Similarity)
		}
	}
}

// TestSearchByVector_DropsArchivedDuringHydration — the vector store keeps
// vectors after an entity is archived, so the SQL hydration step is the only
// place that can filter them out. It must drop them and still return the
// remaining rows in store order.
func TestSearchByVector_DropsArchivedDuringHydration(t *testing.T) {
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	vi := NewInMemoryVectorStore(db, 0)

	for _, v := range []struct {
		id  string
		vec []float32
	}{
		{"live-near", []float32{1, 0}},
		{"archived", []float32{1, 0.1}},
		{"live-far", []float32{0, 1}},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO entities (id, category, content, embedding) VALUES (?, 'world', ?, ?)`,
			v.id, v.id, store.EmbeddingToBytes(v.vec)); err != nil {
			t.Fatalf("seed %s: %v", v.id, err)
		}
		if err := vi.Upsert(ctx, []spi.VectorRecord{{Namespace: spi.DefaultNamespace, ID: v.id, Vector: v.vec}}); err != nil {
			t.Fatalf("upsert %s: %v", v.id, err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE entities SET archived = 1 WHERE id = 'archived'`); err != nil {
		t.Fatalf("archive: %v", err)
	}

	results, err := SearchByVector(ctx, db, vi, []float32{1, 0}, 3)
	if err != nil {
		t.Fatalf("SearchByVector: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 live results, got %d: %+v", len(results), results)
	}
	if results[0].Entity.ID != "live-near" || results[1].Entity.ID != "live-far" {
		t.Fatalf("want [live-near live-far], got %+v", results)
	}
	if results[0].Similarity < 0.99 {
		t.Fatalf("want similarity from the vector store (~1) for live-near, got %v", results[0].Similarity)
	}
}
