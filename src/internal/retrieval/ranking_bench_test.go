package retrieval

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Ranking benchmarks — measure the cost of each ranking strategy and
// the scoring primitives. Run with:
//
//	go test -bench=. -benchmem ./src/internal/retrieval/

func BenchmarkCompositeScore_Default(b *testing.B) {
	w := DefaultRanking{}.Weights()
	scorer := defaultCompositeScorer(w)
	node := core.GraphNode{Entity: core.Entity{ID: "x", Degree: 5}}
	vec := []float32{1, 0, 0}
	query := []float32{1, 0, 0}
	qnorm := vector.VectorNorm(query)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = scorer(node, vec, query, qnorm)
	}
}

func BenchmarkCompositeScore_FreshnessFirst(b *testing.B) {
	w := FreshnessFirst{}.Weights()
	scorer := defaultCompositeScorer(w)
	node := core.GraphNode{Entity: core.Entity{ID: "x", Degree: 5}}
	vec := []float32{1, 0, 0}
	query := []float32{1, 0, 0}
	qnorm := vector.VectorNorm(query)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = scorer(node, vec, query, qnorm)
	}
}

func BenchmarkComputeScoreComponents(b *testing.B) {
	w := core.RankingWeight{}.WithDefaults()
	node := core.GraphNode{Entity: core.Entity{ID: "x", Degree: 10}}
	vec := []float32{1, 0, 0}
	query := []float32{1, 0, 0}
	qnorm := vector.VectorNorm(query)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ComputeScoreComponents(node, vec, query, qnorm, w)
	}
}

func BenchmarkBuildScoreBreakdown(b *testing.B) {
	w := core.RankingWeight{}.WithDefaults()
	c := ScoreComponents{Sim: 0.9, Recency: 0.8, Temporal: 0.7, Centrality: 0.5, Path: 1.0}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildScoreBreakdown(c, w)
	}
}

func BenchmarkDepthDecay(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = depthDecay(2.0)
	}
}

func BenchmarkSortByScoreDesc_100(b *testing.B) {
	ranked := make([]rankedNode, 100)
	for i := range ranked {
		ranked[i] = rankedNode{
			node:  core.GraphNode{Entity: core.Entity{ID: "n"}},
			score: float32(i) / 100,
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Copy to avoid mutating the original (stable sort on already-sorted is fast).
		r := make([]rankedNode, len(ranked))
		copy(r, ranked)
		sortByScoreDesc(r)
	}
}
