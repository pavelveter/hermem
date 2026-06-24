package main

import (
	"fmt"
	"testing"
)

// BenchmarkCommunityDetection measures DetectCommunities wall-clock
// time on random graphs of increasing size. Each graph has
// ~1.5 × N edges (sparse, undirected, random pairings) to match
// typical knowledge-graph topology.
//
// Scale: 100, 500, 1000, 2000 nodes. Sparse graphs keep the
// Louvain inner loop O(N log N) and fit within benchmark timeout.
func BenchmarkCommunityDetection(b *testing.B) {
	for _, n := range []int{100, 500, 1000, 2000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			db, err := InitDB(":memory:", 768)
			if err != nil {
				b.Fatalf("InitDB: %v", err)
			}
			defer db.Close()

			// Create n entities.
			for i := 0; i < n; i++ {
				id := fmt.Sprintf("c%d", i)
				if _, err := db.Exec(
					`INSERT INTO entities (id, category, content) VALUES (?, 'world', ?)`,
					id, id,
				); err != nil {
					b.Fatalf("insert entity %d: %v", i, err)
				}
			}

			// Create ~1.5 × n random undirected edges.
			ne := int(float64(n) * 1.5)
			for e := 0; e < ne; e++ {
				src := fmt.Sprintf("c%d", e%n)
				dst := fmt.Sprintf("c%d", (e*13+7)%n)
				if src == dst {
					dst = fmt.Sprintf("c%d", (e+1)%n)
				}
				if _, err := db.Exec(
					`INSERT OR IGNORE INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, 'related_to', 1.0)`,
					src, dst,
				); err != nil {
					b.Fatalf("insert edge %d: %v", e, err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				communities, _, err := DetectCommunities(db, 50)
				if err != nil {
					b.Fatalf("DetectCommunities: %v", err)
				}
				// Force result use — prevent compiler optimising away.
				_ = communities
			}
		})
	}
}
