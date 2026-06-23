package main

import (
	"fmt"
	"testing"
)

func benchEmbedding(seed int, dim int) []float32 {
	v := make([]float32, dim)
	for d := 0; d < dim; d++ {
		v[d] = float32((seed*(d+1))%1000) / 1000.0
	}
	return v
}

func BenchmarkInMemorySearch(b *testing.B) {
	for _, n := range []int{100, 1000, 5000, 10000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			db, err := InitDB(":memory:", 768)
			if err != nil {
				b.Fatalf("InitDB: %v", err)
			}
			vi := newVectorIndex("in-memory", db, 768)
			defer db.Close()

			for i := 0; i < n; i++ {
				if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
					ID:        fmt.Sprintf("mem-%d", i),
					Category:  "world",
					Content:   fmt.Sprintf("fact-%d", i),
					Embedding: benchEmbedding(i, 768),
				}); err != nil {
					b.Fatalf("store %d: %v", i, err)
				}
			}

			q := benchEmbedding(42, 768)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := SearchByVector(db, vi, q, 10); err != nil {
					b.Fatalf("search: %v", err)
				}
			}
		})
	}
}
