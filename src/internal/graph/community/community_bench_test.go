package community_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/pavelveter/hermem/src/internal/graph/community"
	"github.com/pavelveter/hermem/src/internal/store"
)

func benchGraph(b *testing.B, n, clusters, deg int) *sql.DB {
	b.Helper()
	db, err := store.MemDBRandom()
	if err != nil {
		b.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if _, err := tx.Exec(`INSERT INTO entities (id, category, content) VALUES (?, 'world', 'c')`, fmt.Sprintf("e-%05d", i)); err != nil {
			b.Fatal(err)
		}
	}
	size := n / clusters
	for i := 0; i < n; i++ {
		base := (i / size) * size
		for j := 0; j < deg; j++ {
			dst := base + (i*7+j*13+1)%size
			if dst == i {
				continue
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, 'uses', 1)`, fmt.Sprintf("e-%05d", i), fmt.Sprintf("e-%05d", dst)); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	return db
}

func BenchmarkDetectCommunities(b *testing.B) {
	for _, n := range []int{1000, 3000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			db := benchGraph(b, n, 10, 4)
			defer db.Close()
			g, err := community.LoadGraph(context.Background(), db)
			if err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				community.DetectCommunities(g, 10)
			}
		})
	}
}

func BenchmarkLoadGraph(b *testing.B) {
	db := benchGraph(b, 3000, 10, 4)
	defer db.Close()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := community.LoadGraph(context.Background(), db); err != nil {
			b.Fatal(err)
		}
	}
}
