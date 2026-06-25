package retrieval

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
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
	out, err := expandGraph(db, []string{seedID}, core.RetrieveContextOptions{MaxDepth: 2}, 2)
	if err != nil {
		b.Fatalf("expandGraph: %v", err)
	}
	return out
}

func BenchmarkRetrieveContext(b *testing.B) {
	db, seedID := benchSetup(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = RetrieveContext(db, []string{seedID}, core.RetrieveContextOptions{
			MaxDepth:       2,
			QueryEmbedding: []float32{0.5, 0.5, 0.5},
		})
	}
}

func BenchmarkExpandGraph(b *testing.B) {
	db, seedID := benchSetup(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expandGraph(db, []string{seedID}, core.RetrieveContextOptions{MaxDepth: 2}, 2)
	}
}

func BenchmarkScoreAndRank(b *testing.B) {
	db, seedID := benchSetup(b)
	items := preBuiltScanned(b, db, seedID)
	opts := core.RetrieveContextOptions{
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
	opts := core.RetrieveContextOptions{
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
