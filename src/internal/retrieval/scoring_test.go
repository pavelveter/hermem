package retrieval

import (
	"math"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// (RankingWeight).WithDefaults fills zero fields with the canonical defaults.
func TestWithDefaults_ZeroFieldsGetDefaults(t *testing.T) {
	got := core.RankingWeight{}.WithDefaults()
	if got.VectorWeight != 0.7 {
		t.Fatalf("VectorWeight default: want 0.7, got %v", got.VectorWeight)
	}
	if got.RecencyWeight != 0.3 {
		t.Fatalf("RecencyWeight default: want 0.3, got %v", got.RecencyWeight)
	}
	if got.DepthPenalty != 0.05 {
		t.Fatalf("DepthPenalty default: want 0.05, got %v", got.DepthPenalty)
	}
	if got.RecencyHalfLifeHours != 720 {
		t.Fatalf("RecencyHalfLifeHours default: want 720, got %v", got.RecencyHalfLifeHours)
	}
	if got.TemporalHalfLifeHours != 720 {
		t.Fatalf("TemporalHalfLifeHours default: want 720, got %v", got.TemporalHalfLifeHours)
	}
	if got.CentralityWeight != 0.05 {
		t.Fatalf("CentralityWeight default: want 0.05, got %v", got.CentralityWeight)
	}
}

func TestWithDefaults_NonZeroFieldsPreserved(t *testing.T) {
	in := core.RankingWeight{
		VectorWeight:          0.5,
		RecencyWeight:         0.4,
		DepthPenalty:          0.1,
		RecencyHalfLifeHours:  100,
		TemporalWeight:        0.2,
		TemporalHalfLifeHours: 200,
		CentralityWeight:      0.3,
	}
	got := in.WithDefaults()
	if got.VectorWeight != 0.5 || got.RecencyWeight != 0.4 || got.DepthPenalty != 0.1 {
		t.Fatalf("non-zero fields were zeroed: %+v", got)
	}
	if got.RecencyHalfLifeHours != 100 || got.TemporalHalfLifeHours != 200 ||
		got.CentralityWeight != 0.3 || got.TemporalWeight != 0.2 {
		t.Fatalf("half-life/weight fields drifted: %+v", got)
	}
}

// compositeScore: linear combination with exponential depth decay.
func TestCompositeScore_LinearComb(t *testing.T) {
	w := core.RankingWeight{
		VectorWeight:     0.5,
		RecencyWeight:    0.3,
		TemporalWeight:   0.1,
		CentralityWeight: 0.05,
		DepthPenalty:     0.05,
	}
	// pathWeight=0 → depthDecay=1, no penalty
	got := compositeScore(w, 1.0, 1.0, 1.0, 1.0, 0)
	want := float32(0.5 + 0.3 + 0.1 + 0.05) // no depth penalty when path=0
	if !floatNear(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestCompositeScore_DepthPenaltySubtractive(t *testing.T) {
	w := core.RankingWeight{
		VectorWeight: 1, RecencyWeight: 0, TemporalWeight: 0, CentralityWeight: 0,
		DepthPenalty: 0.5,
	}
	// pathWeight=2 → depthDecay = 2^(-2) = 0.25
	// score = 1.0 * 0.25 = 0.25
	got := compositeScore(w, 1.0, 0, 0, 0, 2.0)
	if !floatNear(got, 0.25) {
		t.Fatalf("exponential decay for pathWeight=2: want 0.25, got %v", got)
	}
}

// sortByScoreDesc: highest score first.
func TestSortByScoreDesc_HighestFirst(t *testing.T) {
	ranked := []rankedNode{
		{node: core.GraphNode{Entity: core.Entity{ID: "c"}}, score: 0.3},
		{node: core.GraphNode{Entity: core.Entity{ID: "a"}}, score: 0.9},
		{node: core.GraphNode{Entity: core.Entity{ID: "b"}}, score: 0.6},
	}
	sortByScoreDesc(ranked)
	want := []string{"a", "b", "c"}
	for i, r := range ranked {
		if r.node.Entity.ID != want[i] {
			t.Fatalf("position %d: want %q, got %q", i, want[i], r.node.Entity.ID)
		}
	}
}

func TestSortByScoreDesc_StableOnTies(t *testing.T) {
	ranked := []rankedNode{
		{node: core.GraphNode{Entity: core.Entity{ID: "x"}}, score: 0.5},
		{node: core.GraphNode{Entity: core.Entity{ID: "y"}}, score: 0.5},
	}
	sortByScoreDesc(ranked)
	if ranked[0].node.Entity.ID != "x" || ranked[1].node.Entity.ID != "y" {
		t.Fatal("stable sort must preserve original order on ties")
	}
}

// defaultCompositeScorer: integration with vector + recency.
func TestDefaultCompositeScorer_UsesVectorAndRecency(t *testing.T) {
	w := core.RankingWeight{
		VectorWeight: 1, RecencyWeight: 0, DepthPenalty: 0,
	}.WithDefaults()
	scorer := defaultCompositeScorer(w)

	oldTime := time.Now().Add(-2000 * time.Hour)
	recentTime := time.Now()

	old := core.GraphNode{
		Entity: core.Entity{ID: "old", UpdatedAt: &oldTime, Degree: 0},
	}
	recent := core.GraphNode{
		Entity: core.Entity{ID: "recent", UpdatedAt: &recentTime, Degree: 0},
	}

	q := []float32{1, 0}
	v := []float32{1, 0}

	gotOld := scorer(old, v, q, vector.VectorNorm(q))
	gotRecent := scorer(recent, v, q, vector.VectorNorm(q))
	// Identical vectors → identical sim, but recency boosts newer.
	if gotRecent <= gotOld {
		t.Fatalf("recent node should outrank equally-relevant old node: gotRecent=%v gotOld=%v", gotRecent, gotOld)
	}
}

// computeRecency / recencyScore: behavior on edge cases.
func TestRecencyScoreUpdatedAt_ZeroIsOne(t *testing.T) {
	if got := recencyScore(nil, 720); got != 1 {
		t.Fatalf("zero UpdatedAt: want 1, got %v", got)
	}
}

func TestRecencyScore_HalfLifeDecay(t *testing.T) {
	old := time.Now().Add(-720 * time.Hour)
	got := recencyScore(&old, 720)
	// expected e^(-1) ≈ 0.3679
	if got < 0.3 || got > 0.4 {
		t.Fatalf("decay at one half-life: want ≈0.368, got %v", got)
	}
}

// temporalScore handles nil/zero inputs.
func TestTemporalScore_NilCreatedAtIsZero(t *testing.T) {
	if got := temporalScore(nil, 720); got != 0 {
		t.Fatalf("nil CreatedAt: want 0, got %v", got)
	}
}

func TestTemporalScore_ZeroCreatedAtIsZero(t *testing.T) {
	zero := time.Time{}
	if got := temporalScore(&zero, 720); got != 0 {
		t.Fatalf("zero CreatedAt: want 0, got %v", got)
	}
}

// centralityScore: log10(1+degree)
func TestCentralityScore_NoDegreeIsZero(t *testing.T) {
	if got := centralityScore(0); got != 0 {
		t.Fatalf("degree <= 0: want 0, got %v", got)
	}
}

func TestCentralityScore_ScalesWithDegree(t *testing.T) {
	small := centralityScore(1)   // log10(2) ≈ 0.301
	large := centralityScore(100) // log10(101) ≈ 2.004
	if large <= small {
		t.Fatalf("centrality should grow with degree: small=%v large=%v", small, large)
	}
}

// floatNear mirrors vector/cosine_test.go helper without duplication. Go in-package
// reuse keeps the test file lean.
func floatNear(a, b float32) bool {
	const tol = float32(1e-5)
	if a == b {
		return true
	}
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < tol
}

// --- ScoreBreakdown helpers ---

func TestComputeScoreComponents_PopulatesAllFields(t *testing.T) {
	now := time.Now()
	created := now.Add(-48 * time.Hour)
	node := core.GraphNode{
		Entity: core.Entity{
			ID:        "x",
			UpdatedAt: &now,
			CreatedAt: &created,
			Degree:    9,
		},
		PathWeight: 1.5,
	}
	w := core.RankingWeight{
		RecencyHalfLifeHours:  720,
		TemporalHalfLifeHours: 720,
	}.WithDefaults()
	nodeVec := []float32{1, 0, 0}
	query := []float32{1, 0, 0}
	c := ComputeScoreComponents(node, nodeVec, query, vector.VectorNorm(query), w)
	// Identical vectors → sim ≈ 1
	if c.Sim < 0.99 {
		t.Fatalf("Sim: want ≈1, got %v", c.Sim)
	}
	// Fresh updated_at → recency ≈ 1
	if c.Recency < 0.99 {
		t.Fatalf("Recency: want ≈1, got %v", c.Recency)
	}
	// 48h old created → small temporal decay (≈ 0.95 with half-life 720)
	if c.Temporal <= 0 || c.Temporal >= 1 {
		t.Fatalf("Temporal: want in (0,1) for 48h old, got %v", c.Temporal)
	}
	// centrality = log10(1 + 9) ≈ 1.0
	if c.Centrality < 0.95 || c.Centrality > 1.05 {
		t.Fatalf("Centrality: want ≈1.0, got %v", c.Centrality)
	}
	// Path is the raw path_weight, untouched.
	if c.Path != 1.5 {
		t.Fatalf("Path: want 1.5, got %v", c.Path)
	}
}

func TestComputeScoreComponents_EmptyQueryYieldsZeroSim(t *testing.T) {
	node := core.GraphNode{Entity: core.Entity{ID: "x"}}
	w := core.RankingWeight{}.WithDefaults()
	c := ComputeScoreComponents(node, []float32{1, 0, 0}, nil, 0, w)
	if c.Sim != 0 {
		t.Fatalf("Sim with nil query: want 0, got %v", c.Sim)
	}
}

func TestBuildScoreBreakdown_FinalMatchesCompositeScore(t *testing.T) {
	w := core.RankingWeight{
		VectorWeight:     0.5,
		RecencyWeight:    0.3,
		TemporalWeight:   0.1,
		CentralityWeight: 0.05,
		DepthPenalty:     0.05,
	}.WithDefaults()
	c := ScoreComponents{
		Sim: 1.0, Recency: 1.0, Temporal: 1.0, Centrality: 1.0, Path: 1.0,
	}
	bd := BuildScoreBreakdown(c, w)
	if bd == nil {
		t.Fatal("BuildScoreBreakdown returned nil")
	}
	wantFinal := compositeScore(w, c.Sim, c.Recency, c.Temporal, c.Centrality, c.Path)
	if !floatNear(bd.FinalScore, wantFinal) {
		t.Fatalf("FinalScore %v != compositeScore %v", bd.FinalScore, wantFinal)
	}
	// Per-field mapping: VectorScore==Sim, DepthPenalty==1-depthDecay(Path)
	if bd.VectorScore != c.Sim {
		t.Fatalf("VectorScore=%v want %v", bd.VectorScore, c.Sim)
	}
	if bd.RecencyScore != c.Recency {
		t.Fatalf("RecencyScore=%v want %v", bd.RecencyScore, c.Recency)
	}
	if bd.TemporalScore != c.Temporal {
		t.Fatalf("TemporalScore=%v want %v", bd.TemporalScore, c.Temporal)
	}
	if bd.CentralityScore != c.Centrality {
		t.Fatalf("CentralityScore=%v want %v", bd.CentralityScore, c.Centrality)
	}
	if bd.PathScore != c.Path {
		t.Fatalf("PathScore=%v want %v", bd.PathScore, c.Path)
	}
	wantDepthPenalty := float32(1 - depthDecay(c.Path))
	if !floatNear(bd.DepthPenalty, wantDepthPenalty) {
		t.Fatalf("DepthPenalty=%v want %v", bd.DepthPenalty, wantDepthPenalty)
	}
}

func TestBuildScoreBreakdown_DepthPenaltySubtractsFromFinal(t *testing.T) {
	// Build a breakdown with high features and a non-zero path;
	// FinalScore = weighted_sum * 2^(-path).
	w := core.RankingWeight{
		VectorWeight: 1.0,
	}.WithDefaults()
	c := ScoreComponents{
		Sim: 0.8, Path: 2.0,
	}
	bd := BuildScoreBreakdown(c, w)
	// depthDecay(2.0) = 0.25, so FinalScore = 0.8 * 0.25 = 0.2
	want := float32(0.2)
	if !floatNear(bd.FinalScore, want) {
		t.Fatalf("FinalScore %v != expected %v (VectorScore*sim * 2^(-path))", bd.FinalScore, want)
	}
}

func TestBuildScoreBreakdown_NaNInfFinalClampedToZero(t *testing.T) {
	// Sim=+Inf drives FinalScore to +Inf through compositeScore; clamp
	// must bring it back to 0 so downstream sort doesn't propagate NaN.
	w := core.RankingWeight{
		VectorWeight: 1.0,
	}.WithDefaults()
	c := ScoreComponents{
		Sim:        float32(math.Inf(1)),
		Recency:    0,
		Temporal:   0,
		Centrality: 0,
		Path:       0,
	}
	bd := BuildScoreBreakdown(c, w)
	if bd.FinalScore != 0 {
		t.Fatalf("+Inf FinalScore: want clamp to 0, got %v", bd.FinalScore)
	}
}
