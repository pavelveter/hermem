package main

import (
	"fmt"
	"testing"
)

// benchmarkRetrieveContext10k measures full-call RetrieveContext
// latency on a 10k-row star graph (one seed fanning out to every other
// entity), so depth=1 visits every row in the corpus — exactly the
// workload for which the post-#17 query-norm precompute amortizes its
// one-time sqrt.
//
// When perRowRecompute is true, the bench injects a custom
// CompositeScorer that pays both sqrts per row via CosineSimilarity,
// exposing the pre-#17 baseline cost. Combined with the post-#17
// default path captured below, both columns in the CHANGELOG
// appendix are reproducible from a single `go test -bench` invocation:
// same harness, same 10k corpus, only the scorer changes.
//
// Star graph shape (1 seed → all others) is chosen over the
// forward-chain used by BenchmarkRetrieveContextN so the CTE produces
// len(corpus) unique rows (rather than 2) and the per-row cosine cost
// is the dominant timed work — not SQLite CTE overhead.
//
// N=500 is a timeout-friendly size that stays under ~30 s for CI
// and shows the cached-vs-recompute gap cleanly. Scale to N=10_000
// for release-bench runs (the per-row delta is linear in N).
//
// Wall-clock figures vary by host (GOOS, Go version, CPU
// micro-architecture); the per-row sqrts vs single-sqrt delta is
// stable. Re-running the bench on the host refreshes both rows in
// the CHANGELOG appendix table.
func benchmarkRetrieveContext10k(b *testing.B, perRowRecompute bool) {
	db, err := InitDB(":memory:", 768)
	if err != nil {
		b.Fatalf("InitDB: %v", err)
	}
	vi := newVectorIndex("in-memory", db, 768)
	defer db.Close()

	const n = 500
	for i := 0; i < n; i++ {
		if err := StoreEntityWithEmbedding(db, vi, Entity{
			ID:        fmt.Sprintf("r10k-%d", i),
			Category:  "world",
			Content:   fmt.Sprintf("fact-%d", i),
			Embedding: benchEmbedding(i, 768),
		}); err != nil {
			b.Fatalf("store %d: %v", i, err)
		}
	}
	// Star graph: r10k-0 → all other entities. At depth=1 the CTE
	// visits every entity exactly once, so we score n rows in the
	// per-call hot loop.
	for i := 1; i < n; i++ {
		if _, err := db.Exec(`INSERT OR IGNORE INTO edges (source_id, target_id, relation_type) VALUES (?,?,?)`,
			"r10k-0", fmt.Sprintf("r10k-%d", i), "related_to"); err != nil {
			b.Fatalf("edge %d: %v", i, err)
		}
	}

	opts := RetrieveContextOptions{
		MaxDepth:       1,
		QueryEmbedding: benchEmbedding(42, 768),
	}
	if perRowRecompute {
		// Recreate the pre-#17 cost by calling CosineSimilarity
		// directly — it re-pays both the query-side and node-side
		// sqrts per row. The 4th arg `queryNorm` is intentionally
		// ignored: that's the no-cache path that pre-#17 callers
		// implicitly relied on.
		//
		// `compositeScore` receives `float32(node.Depth)` so the
		// depth-penalty contribution matches the default scorer
		// exactly; recency=1 matches the default scorer's effective
		// value for freshly-inserted entities with auto-stamped
		// updated_at. The two scorers produce the same numeric
		// output within float32 epsilon.
		opts.CompositeScorer = func(
			node GraphNode,
			nodeVec []float32,
			qEmb []float32,
			_ float32,
		) float32 {
			depth := float32(node.Depth)
			if len(qEmb) == 0 || len(nodeVec) == 0 {
				return compositeScore(0, 1, depth)
			}
			return compositeScore(CosineSimilarity(nodeVec, qEmb), 1, depth)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := RetrieveContext(db, []string{"r10k-0"}, opts); err != nil {
			b.Fatalf("retrieve: %v", err)
		}
	}
}

// BenchmarkRetrieveContextStarPrecompute is the post-#17 production
// path: RetrieveContext uses defaultCompositeScorer (nil opts), which
// calls CosineSimilarityWithNorm with the cached queryNorm computed
// once at the top of RetrieveContext. Pays one sqrt per row (for
// normB) and reuses the cached queryNorm across all rows.
func BenchmarkRetrieveContextStarPrecompute(b *testing.B) {
	benchmarkRetrieveContext10k(b, false)
}

// BenchmarkRetrieveContextStarRecompute is the pre-#17 baseline path:
// a CompositeScorer override calls CosineSimilarity(nodeVec, qEmb)
// directly, re-paying both sqrts per row via one vector and one
// self-dot per row. Mirrors what pre-#17 callers did implicitly when
// defaultCompositeScorer delegated to CosineSimilarity (the function
// before it was split into WithNorm in commit 2c83bfe).
func BenchmarkRetrieveContextStarRecompute(b *testing.B) {
	benchmarkRetrieveContext10k(b, true)
}
