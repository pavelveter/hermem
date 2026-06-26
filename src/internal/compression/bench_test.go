package compression

import (
	"context"
	"fmt"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

func BenchmarkCluster(b *testing.B) {
	db := openTestDB(b)
	cfg := DefaultClustererConfig()
	cfg.SimilarityThreshold = 0.70
	c := NewClusterer(db, cfg)

	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("bench-%d", i)
		emb := make([]float32, 64)
		emb[i%64] = float32(i) / 100
		seedEntityFull(b, db, id, "world", "content", "", zeroTime, emb)
	}

	ids := make([]string, 100)
	for i := range ids {
		ids[i] = fmt.Sprintf("bench-%d", i)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err := c.Cluster(context.Background(), ids)
		if err != nil {
			b.Fatalf("cluster: %v", err)
		}
	}
}

func BenchmarkCompress(b *testing.B) {
	db := openTestDB(b)
	for i := 0; i < 10; i++ {
		seedEntity(b, db, fmt.Sprintf("e-%d", i), "world", "benchmark entity content")
	}

	cp := NewCompressor(db, &mockExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{Category: "world", Content: "benchmark summary"},
			},
		},
	})

	ids := make([]string, 10)
	for i := range ids {
		ids[i] = fmt.Sprintf("e-%d", i)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err := cp.Compress(context.Background(), ids)
		if err != nil {
			b.Fatalf("compress: %v", err)
		}
	}
}

func BenchmarkRecompress(b *testing.B) {
	db := openTestDB(b)
	seedEntity(b, db, "e1", "world", "benchmark entity")

	cp := NewCompressor(db, &mockExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{Category: "world", Content: "summary"},
			},
		},
	})

	first, err := cp.Compress(context.Background(), []string{"e1"})
	if err != nil {
		b.Fatalf("compress: %v", err)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, err := cp.Recompress(context.Background(), first.ID)
		if err != nil {
			b.Fatalf("recompress: %v", err)
		}
	}
}
