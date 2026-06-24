package main

import (
	"context"
	"fmt"
	"testing"
)

// stubBenchEmbedder produces a deterministic 768-dim float32 slice.
// Separate from stubEmbedder (ingestion_test.go, 3-dim) because
// ReEmbedAll's dimension guard would reject 3-dim vectors against
// the 768-dim benchmark target — this stub matches the real workflow.
type stubBenchEmbedder struct{}

func (e *stubBenchEmbedder) Embed(_ context.Context, content string) ([]float32, error) {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32((len(content)*(i+1))%1000) / 1000.0
	}
	return v, nil
}

// BenchmarkReEmbed measures ReEmbedAll wall-clock time on a dataset
// of n entities with content. Uses a stub embedder (no HTTP) so the
// benchmark isolates DB + vector-index write cost.
//
// Scale: 10, 50, 200, 500 entities. All are world-category with
// deterministic content.
func BenchmarkReEmbed(b *testing.B) {
	emb := &stubBenchEmbedder{}
	ctx := context.Background()

	for _, n := range []int{10, 50, 200, 500} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			db, err := InitDB(":memory:", 768)
			if err != nil {
				b.Fatalf("InitDB: %v", err)
			}
			vi := newVectorIndex("in-memory", db, 768)
			defer db.Close()

			// Seed entities with content but no embeddings.
			for i := 0; i < n; i++ {
				if _, err := db.Exec(
					`INSERT INTO entities (id, category, content) VALUES (?, 'world', ?)`,
					fmt.Sprintf("r%d", i), fmt.Sprintf("entity content number %d", i),
				); err != nil {
					b.Fatalf("insert entity %d: %v", i, err)
				}
			}

			// First b.N iteration re-embeds empty entities (INSERT);
			// subsequent iterations measure the UPDATE path (entities
			// already have embeddings, meta.embedding_dim is set).
			// Both paths represent real-world re-embed workloads.
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := ReEmbedAll(ctx, db, vi, emb, 768, n, "")
				if err != nil {
					b.Fatalf("ReEmbedAll: %v", err)
				}
				_ = result
			}
		})
	}
}
