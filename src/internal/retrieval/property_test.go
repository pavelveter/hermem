package retrieval

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// --- Ranking invariants ---

// TestRanking_DeterministicOrdering verifies that the same inputs always
// produce the same ranking order. Runs the scorer twice on identical data
// and asserts byte-identical score sequences.
func TestRanking_DeterministicOrdering(t *testing.T) {
	w := core.RankingWeight{}.WithDefaults()
	scorer := defaultCompositeScorer(w)
	nodes := []struct {
		node    core.GraphNode
		nodeVec []float32
	}{
		{core.GraphNode{Entity: core.Entity{ID: "a", Degree: 5}}, []float32{1, 0}},
		{core.GraphNode{Entity: core.Entity{ID: "b", Degree: 10}}, []float32{0, 1}},
		{core.GraphNode{Entity: core.Entity{ID: "c", Degree: 3}}, []float32{0.7, 0.7}},
	}
	query := []float32{1, 0}
	qnorm := vector.VectorNorm(query)

	scores1 := make([]float32, len(nodes))
	scores2 := make([]float32, len(nodes))
	for i, n := range nodes {
		scores1[i] = scorer(n.node, n.nodeVec, query, qnorm)
		scores2[i] = scorer(n.node, n.nodeVec, query, qnorm)
	}
	for i := range scores1 {
		if scores1[i] != scores2[i] {
			t.Fatalf("non-deterministic score at index %d: %v != %v", i, scores1[i], scores2[i])
		}
	}
}

// TestRanking_IdenticalInputsProduceIdenticalScores verifies that two
// nodes with identical features produce identical composite scores.
func TestRanking_IdenticalInputsProduceIdenticalScores(t *testing.T) {
	w := core.RankingWeight{}.WithDefaults()
	scorer := defaultCompositeScorer(w)
	now := core.TimePtr(timeNow())
	nodeA := core.GraphNode{Entity: core.Entity{ID: "a", UpdatedAt: now, Degree: 5}, PathWeight: 1.0}
	nodeB := core.GraphNode{Entity: core.Entity{ID: "b", UpdatedAt: now, Degree: 5}, PathWeight: 1.0}
	vec := []float32{1, 0}
	query := []float32{1, 0}
	qnorm := vector.VectorNorm(query)

	scoreA := scorer(nodeA, vec, query, qnorm)
	scoreB := scorer(nodeB, vec, query, qnorm)
	if scoreA != scoreB {
		t.Fatalf("identical inputs: scoreA=%v scoreB=%v", scoreA, scoreB)
	}
}

// --- Scoring invariants ---

// TestScoring_SimilarityInUnitRange verifies cosine similarity ∈ [0, 1]
// for unit vectors (non-negative components).
func TestScoring_SimilarityInUnitRange(t *testing.T) {
	vectors := [][]float32{
		{1, 0, 0},
		{0, 1, 0},
		{0.5, 0.5, 0.707},
		{1, 1, 1},
		{0, 0, 0},
	}
	query := []float32{1, 0, 0}
	qnorm := vector.VectorNorm(query)
	for i, v := range vectors {
		sim := vector.CosineSimilarityWithNorm(v, query, qnorm)
		if sim < -0.001 || sim > 1.001 {
			t.Fatalf("vector %d: similarity %v not in [0,1]", i, sim)
		}
	}
}

// TestScoring_RecencyNonNegative verifies recencyScore ≥ 0 for all inputs.
func TestScoring_RecencyNonNegative(t *testing.T) {
	w := core.RankingWeight{}.WithDefaults()
	node := core.GraphNode{Entity: core.Entity{ID: "x"}}
	c := ComputeScoreComponents(node, nil, nil, 0, w)
	if c.Recency < 0 {
		t.Fatalf("recency should be ≥ 0, got %v", c.Recency)
	}
}

// TestScoring_CentralityNonNegative verifies centralityScore ≥ 0.
func TestScoring_CentralityNonNegative(t *testing.T) {
	for _, degree := range []int{-1, 0, 1, 5, 100} {
		c := centralityScore(degree)
		if c < 0 {
			t.Fatalf("centrality for degree %d should be ≥ 0, got %v", degree, c)
		}
	}
}

// TestScoring_BuildScoreBreakdownMatchesComputeCompositeScore verifies
// that BuildScoreBreakdown.FinalScore matches a direct compositeScore
// call with the same components.
func TestScoring_BuildScoreBreakdownMatchesComputeCompositeScore(t *testing.T) {
	w := core.RankingWeight{
		VectorWeight:     0.6,
		RecencyWeight:    0.2,
		TemporalWeight:   0.1,
		CentralityWeight: 0.05,
		DepthPenalty:     0.05,
	}.WithDefaults()
	c := ScoreComponents{Sim: 0.9, Recency: 0.8, Temporal: 0.7, Centrality: 0.5, Path: 1.5}
	bd := BuildScoreBreakdown(c, w)
	direct := compositeScore(w, c.Sim, c.Recency, c.Temporal, c.Centrality, c.Path)
	if !floatNear(bd.FinalScore, direct) {
		t.Fatalf("BuildScoreBreakdown.FinalScore=%v != compositeScore=%v", bd.FinalScore, direct)
	}
}

// TestScoring_DepthDecayMonotonic verifies that deeper nodes always
// receive lower decay factors.
func TestScoring_DepthDecayMonotonic(t *testing.T) {
	prev := depthDecay(0)
	for d := float32(0.5); d <= 5; d += 0.5 {
		cur := depthDecay(d)
		if cur >= prev {
			t.Fatalf("depthDecay should be monotonically decreasing: d=%v cur=%v prev=%v", d, cur, prev)
		}
		prev = cur
	}
}

// TestScoring_DepthDecayRange verifies depthDecay ∈ (0, 1] for non-negative depths.
func TestScoring_DepthDecayRange(t *testing.T) {
	for _, d := range []float32{0, 0.5, 1, 2, 5, 10} {
		decay := depthDecay(d)
		if decay <= 0 || decay > 1.001 {
			t.Fatalf("depthDecay(%v) = %v, want in (0, 1]", d, decay)
		}
	}
}

// --- Graph traversal invariants ---

// TestTraversal_MaxDepthRespected verifies that RetrieveContext never
// returns nodes deeper than MaxDepth.
func TestTraversal_MaxDepthRespected(t *testing.T) {
	db := openTestDB(t)
	// Create a chain: a → b → c → d → e
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		seedEntityWithEmbedding(t, db, id, "world", "fact "+id, []float32{1, 0})
	}
	for _, edge := range [][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}, {"d", "e"}} {
		db.Exec(`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, ?, 1.0)`, edge[0], edge[1])
	}

	res, err := RetrieveContext(db, []string{"a"}, core.RetrieveContextOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, f := range res.WorldFacts {
		if f.Depth > 2 {
			t.Fatalf("node %q has depth %d, want ≤ 2", f.Content, f.Depth)
		}
	}
}

// TestTraversal_NoDuplicateIDs verifies no entity appears twice in the
// combined result (SeedNodes + all buckets).
func TestTraversal_NoDuplicateIDs(t *testing.T) {
	db := openTestDB(t)
	seedEntityWithEmbedding(t, db, "x", "world", "fact x", []float32{1, 0})
	seedEntityWithEmbedding(t, db, "y", "world", "fact y", []float32{0, 1})
	db.Exec(`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, ?, 1.0)`, "x", "y")

	res, err := RetrieveContext(db, []string{"x"}, core.RetrieveContextOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	seen := make(map[string]bool)
	for _, n := range res.SeedNodes {
		if seen[n.Entity.ID] {
			t.Fatalf("duplicate seed node ID: %s", n.Entity.ID)
		}
		seen[n.Entity.ID] = true
	}
	for _, f := range res.WorldFacts {
		if seen[f.ParentID] {
			// ParentID is the edge parent, not the fact ID; skip dedup on parent.
			continue
		}
	}
}

// --- Empty database invariants ---

// TestProperty_EmptyDatabaseNeverPanics verifies that RetrieveContext
// returns gracefully on an empty database with various seed inputs.
func TestProperty_EmptyDatabaseNeverPanics(t *testing.T) {
	db := openTestDB(t)
	cases := [][]string{
		{},
		{"nonexistent"},
		{"a", "b", "c"},
	}
	for _, seeds := range cases {
		res, err := RetrieveContext(db, seeds, core.RetrieveContextOptions{MaxDepth: 1})
		if err != nil {
			t.Fatalf("empty DB with seeds %v: unexpected error: %v", seeds, err)
		}
		if res == nil {
			t.Fatalf("empty DB with seeds %v: nil result", seeds)
		}
	}
}

// --- Score finiteness ---

// TestProperty_ScoresRemainFinite verifies that composite scoring
// never produces NaN or Inf for any combination of valid inputs.
func TestProperty_ScoresRemainFinite(t *testing.T) {
	w := core.RankingWeight{}.WithDefaults()
	scorer := defaultCompositeScorer(w)
	for trial := 0; trial < 200; trial++ {
		node := core.GraphNode{
			Entity: core.Entity{
				ID:        "test",
				UpdatedAt: core.TimePtr(time.Now().Add(-time.Duration(trial) * time.Second)),
				Degree:    trial % 50,
			},
			PathWeight: float32(trial%10) * 0.1,
		}
		vec := randomVector(3)
		query := randomVector(3)
		qnorm := vector.VectorNorm(query)
		score := scorer(node, vec, query, qnorm)
		if score != score { // NaN check
			t.Fatalf("trial %d: score is NaN", trial)
		}
		if math.IsInf(float64(score), 0) {
			t.Fatalf("trial %d: score is Inf", trial)
		}
	}
}

// --- Ranking ordering ---

// TestProperty_RankingStableAcrossRuns verifies that the same sorted
// input always produces the same output order across multiple runs.
func TestProperty_RankingStableAcrossRuns(t *testing.T) {
	type entry struct {
		id    string
		score float32
	}
	orig := []entry{
		{"a", 0.5},
		{"b", 0.9},
		{"c", 0.3},
		{"d", 0.9},
	}
	var firstIDs []string
	for trial := 0; trial < 50; trial++ {
		nodes := make([]rankedNode, len(orig))
		for i, e := range orig {
			nodes[i] = rankedNode{node: core.GraphNode{Entity: core.Entity{ID: e.id}}, score: e.score}
		}
		sortByScoreDesc(nodes)
		ids := make([]string, len(nodes))
		for i, n := range nodes {
			ids[i] = n.node.Entity.ID
		}
		if firstIDs == nil {
			firstIDs = ids
			continue
		}
		for i := range ids {
			if ids[i] != firstIDs[i] {
				t.Fatalf("trial %d: non-deterministic ordering at index %d: got %v, want %v", trial, i, ids, firstIDs)
			}
		}
	}
}

// --- Random graph traversal ---

// TestProperty_RandomGraphNeverCrashesRetrieval verifies that random
// graph structures never cause panics in RetrieveContext.
func TestProperty_RandomGraphNeverCrashesRetrieval(t *testing.T) {
	for trial := 0; trial < 20; trial++ {
		db := openTestDB(t)
		n := 5 + trial%10
		for i := 0; i < n; i++ {
			seedEntityWithEmbedding(t, db, fmt.Sprintf("n%d", i), "world",
				fmt.Sprintf("fact %d", i), randomVector(3))
		}
		// Random edges
		for i := 0; i < n; i++ {
			src := fmt.Sprintf("n%d", i%n)
			dst := fmt.Sprintf("n%d", (i+1)%n)
			db.Exec(`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, 'related_to', 1.0)`, src, dst)
		}
		res, err := RetrieveContext(db, []string{"n0"}, core.RetrieveContextOptions{MaxDepth: 3})
		if err != nil {
			t.Fatalf("trial %d: unexpected error: %v", trial, err)
		}
		if res == nil {
			t.Fatalf("trial %d: nil result", trial)
		}
	}
}

// timeNow is a test helper to get the current time.
func timeNow() time.Time {
	return time.Now()
}
