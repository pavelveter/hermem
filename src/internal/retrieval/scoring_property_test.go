package retrieval

import (
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// TestProperty_SimilarityAlwaysInUnitInterval verifies that cosine similarity
// is always in [0,1] for valid vectors.
func TestProperty_SimilarityAlwaysInUnitInterval(t *testing.T) {
	for trial := 0; trial < 100; trial++ {
		a := randomVector(3)
		b := randomVector(3)
		sim := vector.CosineSimilarityWithNorm(a, b, vector.VectorNorm(b))
		if sim < -0.001 || sim > 1.001 {
			t.Errorf("trial %d: similarity %f not in [0,1]", trial, sim)
		}
	}
}

// TestProperty_RecencyNeverNegative verifies that recencyScore
// never returns a negative value.
func TestProperty_RecencyNeverNegative(t *testing.T) {
	now := time.Now()
	for halfLife := float32(1); halfLife <= 1000; halfLife += 10 {
		// Recent time
		score := recencyScore(core.TimePtr(now), halfLife)
		if score < -0.001 {
			t.Errorf("halfLife=%f: recency %f is negative", halfLife, score)
		}
		// Old time
		old := now.Add(-time.Hour * 1000)
		score = recencyScore(core.TimePtr(old), halfLife)
		if score < -0.001 {
			t.Errorf("halfLife=%f, old time: recency %f is negative", halfLife, score)
		}
		// Nil time
		score = recencyScore(nil, halfLife)
		if score < -0.001 {
			t.Errorf("halfLife=%f, nil time: recency %f is negative", halfLife, score)
		}
	}
}

// TestProperty_CentralityNeverNegative verifies that centralityScore
// never returns a negative value.
func TestProperty_CentralityNeverNegative(t *testing.T) {
	for degree := 0; degree <= 1000; degree += 10 {
		score := centralityScore(degree)
		if score < -0.001 {
			t.Errorf("degree=%d: centrality %f is negative", degree, score)
		}
	}
}

// TestProperty_IncreasingSimilarityNeverDecreasesTotalScore verifies that
// increasing similarity (holding other variables constant) never decreases
// the total composite score.
func TestProperty_IncreasingSimilarityNeverDecreasesTotalScore(t *testing.T) {
	w := core.RankingWeight{
		VectorWeight:          0.7,
		RecencyWeight:         0.3,
		DepthPenalty:          0.05,
		RecencyHalfLifeHours:  720,
		TemporalWeight:        0.1,
		TemporalHalfLifeHours: 720,
		CentralityWeight:      0.05,
	}

	// Fix recency, temporal, centrality, path
	recency := float32(0.8)
	temporal := float32(0.7)
	centrality := float32(0.5)
	path := float32(0.1)

	prevScore := float32(-1)
	for sim := float32(0); sim <= 1.0; sim += 0.01 {
		score := compositeScore(w, sim, recency, temporal, centrality, path)
		if score < prevScore-0.001 {
			t.Errorf("sim=%f: score %f < previous %f", sim, score, prevScore)
		}
		prevScore = score
	}
}

// TestProperty_BuildScoreBreakdownMatchesComputeCompositeScore verifies that
// BuildScoreBreakdown always produces a FinalScore matching compositeScore.
func TestProperty_BuildScoreBreakdownMatchesComputeCompositeScore(t *testing.T) {
	w := core.RankingWeight{
		VectorWeight:          0.7,
		RecencyWeight:         0.3,
		DepthPenalty:          0.05,
		RecencyHalfLifeHours:  720,
		TemporalWeight:        0.1,
		TemporalHalfLifeHours: 720,
		CentralityWeight:      0.05,
	}

	for trial := 0; trial < 100; trial++ {
		c := ScoreComponents{
			Sim:        float32(trial%10) * 0.1,
			Recency:    float32((trial+3)%10) * 0.1,
			Temporal:   float32((trial+5)%10) * 0.1,
			Centrality: float32((trial+7)%10) * 0.1,
			Path:       float32((trial+2)%10) * 0.05,
		}

		breakdown := BuildScoreBreakdown(c, w)
		expected := compositeScore(w, c.Sim, c.Recency, c.Temporal, c.Centrality, c.Path)

		if breakdown.FinalScore != expected {
			t.Errorf("trial %d: breakdown.FinalScore=%f, compositeScore=%f",
				trial, breakdown.FinalScore, expected)
		}
	}
}

// TestProperty_ScoreOrderingIsStable verifies that sortByScoreDesc
// produces stable ordering for equal scores.
func TestProperty_ScoreOrderingIsStable(t *testing.T) {
	for trial := 0; trial < 100; trial++ {
		// Create nodes with deterministic IDs and some equal scores
		nodes := make([]rankedNode, 20)
		for i := range nodes {
			nodes[i] = rankedNode{
				node: core.GraphNode{
					Entity: core.Entity{
						ID:       "node-" + string(rune('a'+i%26)),
						Category: "world",
						Content:  "content",
					},
					Depth: 0,
				},
				score: float32(i%5) * 0.2, // 5 distinct score levels
			}
		}

		// Record original order for each score level
		originalOrder := make(map[float32][]string)
		for _, n := range nodes {
			originalOrder[n.score] = append(originalOrder[n.score], n.node.Entity.ID)
		}

		sortByScoreDesc(nodes)

		// Verify that nodes with the same score maintain their relative order
		for score, ids := range originalOrder {
			var sortedIDs []string
			for _, n := range nodes {
				if n.score == score {
					sortedIDs = append(sortedIDs, n.node.Entity.ID)
				}
			}
			if len(ids) != len(sortedIDs) {
				t.Errorf("trial %d, score %f: count mismatch", trial, score)
				continue
			}
			for i := range ids {
				if ids[i] != sortedIDs[i] {
					t.Errorf("trial %d, score %f: order changed at index %d: %s -> %s",
						trial, score, i, ids[i], sortedIDs[i])
				}
			}
		}
	}
}

// TestProperty_CompositeScoreNeverExceedsSumOfWeights verifies that
// the composite score never exceeds the sum of all positive weights.
func TestProperty_CompositeScoreNeverExceedsSumOfWeights(t *testing.T) {
	for trial := 0; trial < 100; trial++ {
		w := core.RankingWeight{
			VectorWeight:          float32(trial%10) * 0.1,
			RecencyWeight:         float32((trial+1)%10) * 0.1,
			TemporalWeight:        float32((trial+2)%10) * 0.1,
			CentralityWeight:      float32((trial+3)%10) * 0.1,
			DepthPenalty:          float32((trial+4)%10) * 0.05,
			RecencyHalfLifeHours:  720,
			TemporalHalfLifeHours: 720,
		}

		// Max possible score when all inputs are 1.0
		maxScore := w.VectorWeight + w.RecencyWeight + w.TemporalWeight + w.CentralityWeight
		score := compositeScore(w, 1.0, 1.0, 1.0, 1.0, 0) // path=0 to avoid penalty

		if score > maxScore+0.001 {
			t.Errorf("trial %d: score %f exceeds max %f", trial, score, maxScore)
		}
	}
}

// TestProperty_DepthPenaltyNeverIncreasesScore verifies that
// depth penalty always reduces or maintains the score.
func TestProperty_DepthPenaltyNeverIncreasesScore(t *testing.T) {
	w := core.RankingWeight{
		VectorWeight:          0.7,
		RecencyWeight:         0.3,
		DepthPenalty:          0.1,
		RecencyHalfLifeHours:  720,
		TemporalWeight:        0.1,
		TemporalHalfLifeHours: 720,
		CentralityWeight:      0.05,
	}

	for path := float32(0); path <= 10; path += 0.5 {
		scoreNoPenalty := compositeScore(w, 0.5, 0.5, 0.5, 0.5, 0)
		scoreWithPenalty := compositeScore(w, 0.5, 0.5, 0.5, 0.5, path)

		if scoreWithPenalty > scoreNoPenalty+0.001 {
			t.Errorf("path=%f: scoreWithPenalty %f > scoreNoPenalty %f",
				path, scoreWithPenalty, scoreNoPenalty)
		}
	}
}

// randomVector creates a deterministic pseudo-random vector for testing.
func randomVector(n int) []float32 {
	vec := make([]float32, n)
	for i := range vec {
		vec[i] = float32(i%10) * 0.1
	}
	return vec
}
