package vector

import (
	"context"
	"errors"
	"testing"

	"github.com/pavelveter/hermem/pkg/spi"
)

// The IDs-only InMemoryVectorIndex was deleted with task 6.4; its
// limit-validation regression tests (CodeQL go/uncontrolled-allocation-size)
// move here, retargeted at the surviving public-contract surfaces that
// accept caller-controlled limits: the in-memory VectorStore's Search.
// SearchByVector clamps topK to MaxResultsCap by construction and takes no
// unvalidated allocation from the caller.

func TestInMemoryVectorStore_SearchRejectsNegativeLimit(t *testing.T) {
	t.Parallel()
	store := NewInMemoryVectorStore(nil, 0)
	if err := store.Upsert(context.Background(), []spi.VectorRecord{
		{Namespace: spi.DefaultNamespace, ID: "x", Vector: []float32{1, 0}},
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := store.Search(context.Background(), spi.SearchRequest{
		Namespace: spi.DefaultNamespace,
		Vector:    []float32{1, 0},
		Limit:     -1,
	})
	if !errors.Is(err, ErrLimitOutOfRange) {
		t.Fatalf("want ErrLimitOutOfRange, got %v", err)
	}
	if hits != nil {
		t.Fatalf("want nil hits on error, got %v", hits)
	}
}

func TestInMemoryVectorStore_SearchZeroAndPositiveLimits(t *testing.T) {
	t.Parallel()
	store := NewInMemoryVectorStore(nil, 0)
	records := []spi.VectorRecord{
		{Namespace: spi.DefaultNamespace, ID: "a", Vector: []float32{1, 0}},
		{Namespace: spi.DefaultNamespace, ID: "b", Vector: []float32{0, 1}},
	}
	if err := store.Upsert(context.Background(), records); err != nil {
		t.Fatal(err)
	}

	search := func(limit int) []spi.Hit {
		t.Helper()
		hits, err := store.Search(context.Background(), spi.SearchRequest{
			Namespace: spi.DefaultNamespace,
			Vector:    []float32{1, 0},
			Limit:     limit,
		})
		if err != nil {
			t.Fatalf("limit=%d: unexpected error: %v", limit, err)
		}
		return hits
	}

	if got := search(0); len(got) != 0 {
		t.Fatalf("limit=0: want no hits, got %v", got)
	}
	hits := search(5)
	if len(hits) != 2 || hits[0].ID != "a" {
		t.Fatalf("limit=5: want [a b], got %v", hits)
	}
}
