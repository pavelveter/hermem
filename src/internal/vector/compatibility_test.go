package vector

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

// This compile-time assertion is intentionally against the old interface:
// compatibility requires preserving its method set, not extending it.
var _ core.VectorIndex = (*InMemoryVectorIndex)(nil)

func TestLegacyVectorIndexRemainsSingleNamespaceCompatibilityView(t *testing.T) {
	index := NewInMemoryVectorIndex(nil)
	if err := index.Store(context.Background(), "id", []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	ids, err := index.Search(context.Background(), []float32{1, 0}, 1)
	if err != nil || len(ids) != 1 || ids[0] != "id" {
		t.Fatalf("legacy IDs-only behavior changed: ids=%v err=%v", ids, err)
	}
}
