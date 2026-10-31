package retrieval

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func TestProbeStoreSearch(t *testing.T) {
	db := openTestDB(t)
	seedEntityWithEmbedding(t, db, "a", "world", "a-content", []float32{1, 0, 0})
	vi := vector.NewInMemoryVectorStore(db, 0)
	if err := vi.Upsert(t.Context(), []spi.VectorRecord{
		{Namespace: spi.DefaultNamespace, ID: "a", Vector: []float32{1, 0, 0}},
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := vi.Search(context.Background(), spi.SearchRequest{Namespace: spi.DefaultNamespace, Vector: []float32{1, 0, 0}, Limit: 3})
	t.Logf("hits=%v err=%v", hits, err)
	if err != nil || len(hits) == 0 {
		t.Fatalf("expected hit")
	}
}
