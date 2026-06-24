package retrieval

import (
	"math"
	"sort"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// rankedNode pairs a graph node with its composite score and (optional) explain fields.
type rankedNode struct {
	node    core.GraphNode
	score   float32
	sim     float32
	recency float32
}

// resolvedRankingWeight returns w with zero fields replaced by defaults.
func resolvedRankingWeight(w core.RankingWeight) core.RankingWeight {
	if w.VectorWeight == 0 {
		w.VectorWeight = 0.7
	}
	if w.RecencyWeight == 0 {
		w.RecencyWeight = 0.3
	}
	if w.DepthPenalty == 0 {
		w.DepthPenalty = 0.05
	}
	if w.RecencyHalfLifeHours == 0 {
		w.RecencyHalfLifeHours = 720
	}
	if w.TemporalHalfLifeHours == 0 {
		w.TemporalHalfLifeHours = 720
	}
	if w.CentralityWeight == 0 {
		w.CentralityWeight = 0.05
	}
	return w
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
	return w.VectorWeight*sim + w.RecencyWeight*recency + w.TemporalWeight*temporalBoost + w.CentralityWeight*centrality - w.DepthPenalty*pathWeight
}

func sortByScoreDesc(ranked []rankedNode) {
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
}

// --- helpers used by walk.go ---

func vectorNormForQuery(q []float32) float32 {
	if len(q) == 0 {
		return 0
	}
	return vector.VectorNorm(q)
}

func decodeNodeVec(blob []byte, dim int) []float32 {
	if len(blob) == 0 || dim == 0 {
		return nil
	}
	v, err := store.DecodeVector(blob, dim)
	if err != nil {
		return nil
	}
	return v
}

func computeSim(nodeVec, queryEmbedding []float32, queryNorm float32) float32 {
	if len(queryEmbedding) == 0 || len(nodeVec) == 0 {
		return 0
	}
	return vector.CosineSimilarityWithNorm(nodeVec, queryEmbedding, queryNorm)
}

func recencyScore(updatedAt time.Time, halfLifeHours float32) float32 {
	if updatedAt.IsZero() || halfLifeHours <= 0 {
		return 1
	}
	hoursOld := float32(time.Since(updatedAt).Hours())
	if hoursOld <= 0 {
		return 1
	}
	return float32(math.Exp(-float64(hoursOld) / float64(halfLifeHours)))
}

func temporalScore(createdAt *time.Time, halfLifeHours float32) float32 {
	if createdAt == nil || createdAt.IsZero() || halfLifeHours <= 0 {
		return 0
	}
	hoursOld := float32(time.Since(*createdAt).Hours())
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
