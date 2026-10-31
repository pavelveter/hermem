package vector

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/store"
)

func TestHybridSearch_RRFAndFallback(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	vi := NewInMemoryVectorStore(db, 0)

	for _, e := range []struct {
		id, content string
		emb         []float32
	}{
		{"a", "alpha beta", []float32{1, 0, 0}},
		{"b", "gamma delta", []float32{0, 1, 0}},
	} {
		blob := store.EmbeddingToBytes(e.emb)
		if _, err := db.Exec(`INSERT INTO entities (id, category, content, embedding) VALUES (?,?,?,?)`, e.id, "world", e.content, blob); err != nil {
			t.Fatal(err)
		}
		if err := vi.Upsert(context.Background(), []spi.VectorRecord{{Namespace: spi.DefaultNamespace, ID: e.id, Vector: e.emb}}); err != nil {
			t.Fatal(err)
		}
	}

	// Fused result contains the lexically-matching entity with combined score.
	res, err := HybridSearch(context.Background(), db, vi, "alpha", []float32{1, 0, 0}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 || res[0].ID != "a" {
		t.Fatalf("want a first, got %+v", res)
	}
	if res[0].Score <= 1.0/(rrfK+1) {
		t.Fatalf("expected fused score > single-channel, got %v", res[0].Score)
	}

	// Vector engine failure does not block keyword results.
	res2, err := HybridSearch(context.Background(), db, nil, "alpha", nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2) == 0 || res2[0].ID != "a" {
		t.Fatalf("lexical fallback failed: %+v", res2)
	}
}