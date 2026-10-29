package retrieval

import (
	"database/sql"
	"fmt"
	"testing"
)

// Per-stage retrieval benchmarks — let operators measure the cost of
// each pipeline stage in isolation.
//
// Run with:
//
//	go test -bench=. -benchmem ./src/internal/retrieval/
//
// Each benchmark seeds a small synthetic graph (50 entities in a
// ring topology) once via benchSetup, then drops the setup cost via
// b.ResetTimer() so the reported ns/op reflects only the stage
// under test.

const benchFixtureNodes = 50

// benchSetup seeds an in-memory SQLite with benchFixtureNodes
// entities chained in a ring, plus 3-dim embeddings (matches the
// MemDB() default dimension). Returns the DB and one seed ID to
// walk from.
func benchSetup(b *testing.B) (*sql.DB, string) {
	b.Helper()
	db := openTestDB(b)
	for i := 0; i < benchFixtureNodes; i++ {
		id := fmt.Sprintf("bench-%d", i)
		emb := []float32{float32(i%5) / 5, float32((i+1)%5) / 5, float32((i+2)%5) / 5}
		seedEntityWithEmbedding(b, db, id, "world", fmt.Sprintf("bench-fact-%d", i), emb)
	}
	for i := 0; i < benchFixtureNodes; i++ {
		next := (i + 1) % benchFixtureNodes
		seedEdge(b, db, fmt.Sprintf("bench-%d", i), fmt.Sprintf("bench-%d", next), "uses")
	}
	return db, "bench-0"
}

// preBuiltScanned returns the expandGraph output for a fresh fixture
// in []scannedNode form, so the scoreAndRank / bucketize benchmarks
// can drive their stage without re-running upstream setup.
func preBuiltScanned(b *testing.B, db *sql.DB, seedID string) []scannedNode {
	b.Helper()
	out, err := expandGraph(db, []string{seedID}, RetrieveContextOptions{MaxDepth: 2}, 2)
	if err != nil {
		b.Fatalf("expandGraph: %v", err)
	}
	return out
}

func BenchmarkRetrieveContext(b *testing.B) {
	db, seedID := benchSetup(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = RetrieveContext(db, []string{seedID}, RetrieveContextOptions{
			MaxDepth:       2,
			QueryEmbedding: []float32{0.5, 0.5, 0.5},
		})
	}
}

func BenchmarkExpandGraph(b *testing.B) {
	db, seedID := benchSetup(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expandGraph(db, []string{seedID}, RetrieveContextOptions{MaxDepth: 2}, 2)
	}
}

// benchScalingNodes sizes the graph-expansion scaling fixture. The 50-node
// fixture above is too small to expose a bad recursive-CTE plan.
const benchScalingNodes = 1000

// BenchmarkExpandGraphScaling guards the recursive CTE query plan. The walk
// must resolve neighbours through the edges source/target indexes and entities
// by primary key; if the entity join degenerates into a scan, this benchmark
// jumps from sub-millisecond to ~19ms on this fixture.
func BenchmarkExpandGraphScaling(b *testing.B) {
	db := openTestDB(b)
	for i := 0; i < benchScalingNodes; i++ {
		seedEntityWithEmbedding(b, db, fmt.Sprintf("scale-%d", i), "world",
			fmt.Sprintf("scale-fact-%d", i), []float32{float32(i%5) / 5, 0.5, 0.5})
	}
	for i := 0; i < benchScalingNodes; i++ {
		for _, d := range []int{1, 7, 13} {
			seedEdge(b, db, fmt.Sprintf("scale-%d", i), fmt.Sprintf("scale-%d", (i+d)%benchScalingNodes), "related_to")
		}
	}
	seeds := []string{"scale-0", "scale-1", "scale-2"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := expandGraph(db, seeds, RetrieveContextOptions{MaxDepth: 2}, 2); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScoreAndRank(b *testing.B) {
	db, seedID := benchSetup(b)
	items := preBuiltScanned(b, db, seedID)
	opts := RetrieveContextOptions{
		QueryEmbedding: []float32{0.5, 0.5, 0.5},
	}
	w := opts.RankingWeight.WithDefaults()
	scorer := defaultCompositeScorer(w)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scoreAndRank(items, opts, w, scorer)
	}
}

func BenchmarkBucketize(b *testing.B) {
	db, seedID := benchSetup(b)
	items := preBuiltScanned(b, db, seedID)
	opts := RetrieveContextOptions{
		QueryEmbedding: []float32{0.5, 0.5, 0.5},
	}
	w := opts.RankingWeight.WithDefaults()
	scorer := defaultCompositeScorer(w)
	ranked, seeds := scoreAndRank(items, opts, w, scorer)
	sortByScoreDesc(ranked)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bucketize(ranked, seeds, w, false)
	}
}
