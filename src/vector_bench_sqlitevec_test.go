//go:build sqlite_vec

package main

import (
	"fmt"
	"testing"
)

func BenchmarkSqliteVecSearch(b *testing.B) {
	for _, n := range []int{100, 1000, 5000, 10000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			db, err := InitDB(":memory:", 768)
			if err != nil {
				b.Fatalf("InitDB: %v", err)
			}
			vi := newVectorIndex("sqlite-vec", db, 768)
			defer db.Close()

			for i := 0; i < n; i++ {
				if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
					ID:        fmt.Sprintf("vec-%d", i),
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
