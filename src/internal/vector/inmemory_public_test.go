package vector

import (
	"context"
	"errors"
	"testing"

	"github.com/pavelveter/hermem/pkg/spi"
)

func TestInMemoryVectorStore_NamespaceFilterAndScores(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryVectorStore(nil, 10)

	if err := store.Upsert(ctx, []spi.VectorRecord{
		{Namespace: "tenant-a/model-1", ID: "world", Vector: []float32{1, 0}, Metadata: map[string]string{"category": "world", "rank": "10"}},
		{Namespace: "tenant-a/model-1", ID: "task", Vector: []float32{0, 1}, Metadata: map[string]string{"category": "task", "rank": "1"}},
		{Namespace: "tenant-b/model-1", ID: "other", Vector: []float32{1, 0}, Metadata: map[string]string{"category": "world", "rank": "10"}},
	}); err != nil {
		t.Fatal(err)
	}

	minRank := 5.0
	hits, err := store.Search(ctx, spi.SearchRequest{
		Namespace: "tenant-a/model-1",
		Vector:    []float32{1, 0},
		Limit:     5,
		Filter: spi.Filter{
			Equals: map[string]string{"category": "world"},
			Ranges: map[string]spi.NumericRange{"rank": {Min: &minRank}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "world" {
		t.Fatalf("filtered hits = %+v", hits)
	}
	if hits[0].Score < 0.99 {
		t.Fatalf("expected cosine score near 1, got %f", hits[0].Score)
	}

	otherHits, err := store.Search(ctx, spi.SearchRequest{
		Namespace: "tenant-b/model-1",
		Vector:    []float32{1, 0},
		Limit:     5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherHits) != 1 || otherHits[0].ID != "other" {
		t.Fatalf("namespace isolation failed: %+v", otherHits)
	}
}

func TestInMemoryVectorStore_UpsertIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryVectorStore(nil, 10)
	record := spi.VectorRecord{Namespace: "model", ID: "same", Vector: []float32{1, 0}}
	if err := store.Upsert(ctx, []spi.VectorRecord{record}); err != nil {
		t.Fatal(err)
	}
	record.Vector = []float32{0, 1}
	if err := store.Upsert(ctx, []spi.VectorRecord{record}); err != nil {
		t.Fatal(err)
	}
	stats, err := store.Stats(ctx, "model")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Count != 1 {
		t.Fatalf("count after repeated upsert = %d, want 1", stats.Count)
	}
	hits, err := store.Search(ctx, spi.SearchRequest{Namespace: "model", Vector: []float32{0, 1}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Score < 0.99 {
		t.Fatalf("updated vector was not visible: %+v", hits)
	}
}

func TestInMemoryVectorStore_ReportsDimensionAndPartialFailures(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryVectorStore(nil, 10)
	if err := store.Upsert(ctx, []spi.VectorRecord{{Namespace: "model", ID: "seed", Vector: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}

	err := store.Upsert(ctx, []spi.VectorRecord{
		{Namespace: "model", ID: "valid", Vector: []float32{0, 1}},
		{Namespace: "model", ID: "invalid", Vector: []float32{1, 0, 0}},
	})
	if err == nil {
		t.Fatal("expected partial batch error")
	}
	var partial *spi.PartialBatchError
	if !errors.As(err, &partial) || len(partial.FailedIDs) != 1 || partial.FailedIDs[0] != "invalid" {
		t.Fatalf("partial error = %+v", err)
	}
	var dimension *spi.DimensionError
	if !errors.As(err, &dimension) {
		t.Fatalf("expected dimension cause, got %v", err)
	}

	stats, statsErr := store.Stats(ctx, "model")
	if statsErr != nil {
		t.Fatal(statsErr)
	}
	if stats.Count != 2 {
		t.Fatalf("valid record was not retained after partial failure: %+v", stats)
	}
}

func TestInMemoryVectorStore_ReportsCapacityInsteadOfEvicting(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryVectorStore(nil, 1)
	if err := store.Upsert(ctx, []spi.VectorRecord{{Namespace: "model", ID: "first", Vector: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}

	err := store.Upsert(ctx, []spi.VectorRecord{{Namespace: "model", ID: "second", Vector: []float32{0, 1}}})
	if err == nil {
		t.Fatal("expected capacity error")
	}
	var capacity *spi.CapacityError
	if !errors.As(err, &capacity) {
		t.Fatalf("expected capacity cause, got %v", err)
	}
	stats, statsErr := store.Stats(ctx, "model")
	if statsErr != nil {
		t.Fatal(statsErr)
	}
	if stats.Count != 1 {
		t.Fatalf("capacity failure evicted or added a record: %+v", stats)
	}
}
