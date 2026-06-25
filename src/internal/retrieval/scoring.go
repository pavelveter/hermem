package retrieval

import (
	"math"
	"sort"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// rankedNode pairs a graph node with its composite score and (optional) explain fields.
type rankedNode struct {
	node    core.GraphNode
	score   float32
	sim     float32
	recency float32
}

// defaultCompositeScorer returns a Scorer implementing the canonical formula.
func defaultCompositeScorer(w core.RankingWeight) core.CompositeScorer {
	return func(node core.GraphNode, nodeVec []float32, queryEmbedding []float32, queryNorm float32) float32 {
		var sim float32
		if len(queryEmbedding) > 0 && len(nodeVec) > 0 {
			sim = vector.CosineSimilarityWithNorm(nodeVec, queryEmbedding, queryNorm)
		}
		recency := recencyScore(node.Entity.UpdatedAt, w.RecencyHalfLifeHours)
		temporal := temporalScore(node.Entity.CreatedAt, w.TemporalHalfLifeHours)
		centrality := centralityScore(node.Entity.Degree)
		return compositeScore(w, sim, recency, temporal, centrality, node.PathWeight)
	}
}

// compositeScore computes the linear combination of features minus depth penalty.
func compositeScore(w core.RankingWeight, sim, recency, temporalBoost, centrality, pathWeight float32) float32 {
	s := w.VectorWeight*sim + w.RecencyWeight*recency + w.TemporalWeight*temporalBoost + w.CentralityWeight*centrality - w.DepthPenalty*pathWeight
	if math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) {
		return 0
	}
	return s
}

// ScoreComponents holds the raw feature values used by compositeScore.
// These are the values that get packed into core.ScoreBreakdown for
// explainability — keep the struct flat so call sites can populate it
// from one set of intermediate computations.
type ScoreComponents struct {
	Sim       float32 // cosine similarity to query
	Recency   float32 // exp-decay on UpdatedAt
	Temporal  float32 // exp-decay on CreatedAt
	Centrality float32 // log10(1 + Degree)
	Path      float32 // cumulative edge weight (path_weight)
}

// ComputeScoreComponents builds the raw feature vector for a node in
// one call. Empty query embeddings yield Sim=0 (no vector signal);
// missing/zero timestamps yield their respective defaults.
func ComputeScoreComponents(node core.GraphNode, nodeVec []float32, queryEmbedding []float32, queryNorm float32, w core.RankingWeight) ScoreComponents {
	var sim float32
	if len(queryEmbedding) > 0 && len(nodeVec) > 0 {
		sim = vector.CosineSimilarityWithNorm(nodeVec, queryEmbedding, queryNorm)
	}
	return ScoreComponents{
		Sim:        sim,
		Recency:    recencyScore(node.Entity.UpdatedAt, w.RecencyHalfLifeHours),
		Temporal:   temporalScore(node.Entity.CreatedAt, w.TemporalHalfLifeHours),
		Centrality: centralityScore(node.Entity.Degree),
		Path:       node.PathWeight,
	}
}

// BuildScoreBreakdown converts raw ScoreComponents into the public
// core.ScoreBreakdown shape (weights × features, depth penalty
// subtracted, NaN/Inf clamped). Used by walk.go to populate the
// explain fields on GraphNode / RetrievedFact when Explain=true.
func BuildScoreBreakdown(c ScoreComponents, w core.RankingWeight) *core.ScoreBreakdown {
	final := compositeScore(w, c.Sim, c.Recency, c.Temporal, c.Centrality, c.Path)
	return &core.ScoreBreakdown{
		VectorScore:     c.Sim,
		RecencyScore:    c.Recency,
		TemporalScore:   c.Temporal,
		CentralityScore: c.Centrality,
		PathScore:       c.Path,
		DepthPenalty:    w.DepthPenalty * c.Path,
		FinalScore:      final,
	}
}

func sortByScoreDesc(ranked []rankedNode) {
	for i := range ranked {
		if math.IsNaN(float64(ranked[i].score)) || math.IsInf(float64(ranked[i].score), 0) {
			ranked[i].score = 0
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
}

// --- scoring helpers used by walk.go + tests ---

func recencyScore(updatedAt time.Time, halfLifeHours float32) float32 {
	if updatedAt.IsZero() || halfLifeHours <= 0 {
		return 1
	}
	hoursOld := float32(time.Since(updatedAt.UTC()).Hours())
	if hoursOld <= 0 {
		return 1
	}
	return float32(math.Exp(-float64(hoursOld) / float64(halfLifeHours)))
}

func temporalScore(createdAt *time.Time, halfLifeHours float32) float32 {
	if createdAt == nil || createdAt.IsZero() || halfLifeHours <= 0 {
		return 0
	}
	hoursOld := float32(time.Since(createdAt.UTC()).Hours())
	if hoursOld <= 0 {
		return 1
	}
	return float32(math.Exp(-float64(hoursOld) / float64(halfLifeHours)))
}

func centralityScore(degree int) float32 {
	if degree <= 0 {
		return 0
	}
	return float32(math.Log10(float64(1 + degree)))
}
