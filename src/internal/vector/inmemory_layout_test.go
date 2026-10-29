package vector

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/pkg/spi"
)

// TestInMemoryVectorStore_DeleteKeepsRowsConsistent guards the contiguous
// matrix layout: Delete is a swap-remove, so IDs, vectors and metadata must
// stay aligned — otherwise searches silently score one entity's vector and
// return another entity's ID or metadata.
func TestInMemoryVectorStore_DeleteKeepsRowsConsistent(t *testing.T) {
	ctx := context.Background()
	vi := NewInMemoryVectorStore(nil, 0)
	if err := vi.Upsert(ctx, []spi.VectorRecord{
		{Namespace: "ns", ID: "a", Vector: []float32{1, 0}, Metadata: map[string]string{"tag": "a"}},
		{Namespace: "ns", ID: "b", Vector: []float32{0, 1}, Metadata: map[string]string{"tag": "b"}},
		{Namespace: "ns", ID: "c", Vector: []float32{1, 1}, Metadata: map[string]string{"tag": "c"}},
		{Namespace: "ns", ID: "d", Vector: []float32{1, -1}, Metadata: map[string]string{"tag": "d"}},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Deleting the second row forces the last row to move into its slot.
	if err := vi.Delete(ctx, spi.DeleteRequest{Namespace: "ns", IDs: []string{"b"}}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Deleting an absent ID stays a no-op.
	if err := vi.Delete(ctx, spi.DeleteRequest{Namespace: "ns", IDs: []string{"ghost"}}); err != nil {
		t.Fatalf("delete missing: %v", err)
	}

	hits, err := vi.Search(ctx, spi.SearchRequest{Namespace: "ns", Vector: []float32{1, -1}, Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("want 3 remaining hits, got %d: %+v", len(hits), hits)
	}
	if hits[0].ID != "d" || hits[0].Score < 0.99 {
		t.Fatalf("want d scored ~1 first, got %+v", hits)
	}

	// Each surviving ID must still carry its own metadata.
	for _, tag := range []string{"a", "c", "d"} {
		filtered, err := vi.Search(ctx, spi.SearchRequest{
			Namespace: "ns",
			Vector:    []float32{1, 0},
			Limit:     10,
			Filter:    spi.Filter{Equals: map[string]string{"tag": tag}},
		})
		if err != nil {
			t.Fatalf("filtered search %s: %v", tag, err)
		}
		if len(filtered) != 1 || filtered[0].ID != tag {
			t.Fatalf("filter tag=%s: want only %s, got %+v", tag, tag, filtered)
		}
	}
	if filtered, _ := vi.Search(ctx, spi.SearchRequest{
		Namespace: "ns",
		Vector:    []float32{1, 0},
		Limit:     10,
		Filter:    spi.Filter{Equals: map[string]string{"tag": "b"}},
	}); len(filtered) != 0 {
		t.Fatalf("deleted row still matches its filter: %+v", filtered)
	}

	// Re-inserting a deleted ID appends a fresh row that scores correctly.
	if err := vi.Upsert(ctx, []spi.VectorRecord{
		{Namespace: "ns", ID: "b", Vector: []float32{0, 1}, Metadata: map[string]string{"tag": "b"}},
	}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	hits, err = vi.Search(ctx, spi.SearchRequest{Namespace: "ns", Vector: []float32{0, 1}, Limit: 10})
	if err != nil {
		t.Fatalf("search after re-upsert: %v", err)
	}
	if len(hits) != 4 || hits[0].ID != "b" || hits[0].Score < 0.99 {
		t.Fatalf("after re-upsert: want b first of 4, got %+v", hits)
	}
}

// TestInMemoryVectorStore_DeleteFreesCapacity — the capacity check must count
// live rows, so removing one makes room for another.
func TestInMemoryVectorStore_DeleteFreesCapacity(t *testing.T) {
	ctx := context.Background()
	vi := NewInMemoryVectorStore(nil, 1)
	if err := vi.Upsert(ctx, []spi.VectorRecord{{Namespace: "ns", ID: "a", Vector: []float32{1, 0}}}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := vi.Delete(ctx, spi.DeleteRequest{Namespace: "ns", IDs: []string{"a"}}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := vi.Upsert(ctx, []spi.VectorRecord{{Namespace: "ns", ID: "b", Vector: []float32{0, 1}}}); err != nil {
		t.Fatalf("upsert after delete: %v", err)
	}
	stats, err := vi.Stats(ctx, "ns")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Count != 1 {
		t.Fatalf("want 1 live record, got %d", stats.Count)
	}
}

func TestTopHits_OrdersAndTruncates(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	scores := []float32{0.1, 0.9, 0.5, 0.7}

	cases := []struct {
		name string
		k    int
		want []string
	}{
		{"top two", 2, []string{"b", "d"}},
		{"all", 4, []string{"b", "d", "c", "a"}},
		{"more than rows", 9, []string{"b", "d", "c", "a"}},
		{"none", 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := topHits(ids, scores, tc.k)
			if len(got) != len(tc.want) {
				t.Fatalf("want %d hits, got %d: %+v", len(tc.want), len(got), got)
			}
			for i, id := range tc.want {
				if got[i].ID != id {
					t.Fatalf("position %d: want %q, got %q (all: %+v)", i, id, got[i].ID, got)
				}
			}
		})
	}
}
