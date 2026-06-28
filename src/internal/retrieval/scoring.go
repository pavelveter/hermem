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
// Implementation delegates to ComputeScoreComponents so the raw-feature
// extraction lives in exactly one place; previously the scorer body and
// ComputeScoreComponents both recomputed sim/recency/temporal/centrality
// independently.
func defaultCompositeScorer(w core.RankingWeight) core.CompositeScorer {
	return func(node core.GraphNode, nodeVec []float32, queryEmbedding []float32, queryNorm float32) float32 {
		comps := ComputeScoreComponents(node, nodeVec, queryEmbedding, queryNorm, w)
		return compositeScore(w, comps.Sim, comps.Recency, comps.Temporal, comps.Centrality, comps.Path)
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
	Sim        float32 // cosine similarity to query
	Recency    float32 // exp-decay on UpdatedAt
	Temporal   float32 // exp-decay on CreatedAt
	Centrality float32 // log10(1 + Degree)
	Path       float32 // cumulative edge weight (path_weight)
}

// Final returns the composite ranking score for these components
// using the provided weights. Lets call sites that already hold a
// ScoreComponents derive the final score without re-running the
// weighted sum themselves — used by walk.go on the Explain path so
// sim/recency/temporal/centrality are computed exactly once per node.
func (c ScoreComponents) Final(w core.RankingWeight) float32 {
	return compositeScore(w, c.Sim, c.Recency, c.Temporal, c.Centrality, c.Path)
}

// ComputeScoreComponents builds the raw feature vector for a node in
// one call. Empty query embeddings yield Sim=0 (no vector signal);
// missing/zero timestamps yield their respective defaults.
//
// Single source of truth for raw-feature extraction — defaultCompositeScorer,
// walk.go Explain path, and any future caller should funnel through here so
// the per-node feature arithmetic stays in lockstep with ScoreBreakdown
// semantics.
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
	weightsCopy := w
	return &core.ScoreBreakdown{
		VectorScore:     c.Sim,
		RecencyScore:    c.Recency,
		TemporalScore:   c.Temporal,
		CentralityScore: c.Centrality,
		PathScore:       c.Path,
		DepthPenalty:    w.DepthPenalty * c.Path,
		FinalScore:      final,
		Weights:         &weightsCopy,
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

// recencyScore — exp-decay on UpdatedAt. Nil/zero UpdatedAt → 1
// ("as fresh as possible"), the conventional recency default so a
// never-touched node doesn't get punished by the ranker.
//
// Implementation delegates to expDecayHours in temporal.go — keeps
// the decay math in exactly one place across recency + temporal.
func recencyScore(updatedAt *time.Time, halfLifeHours float32) float32 {
	if updatedAt == nil || updatedAt.IsZero() || halfLifeHours <= 0 {
		return 1
	}
	return expDecayHours(*updatedAt, halfLifeHours)
}

func centralityScore(degree int) float32 {
	if degree <= 0 {
		return 0
	}
	return float32(math.Log10(float64(1 + degree)))
}
