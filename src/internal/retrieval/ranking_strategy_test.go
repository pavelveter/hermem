package retrieval

import (
	"testing"
)

func TestRankingStrategyByName_Default(t *testing.T) {
	s := RankingStrategyByName("")
	if s.Name() != "default" {
		t.Fatalf("want 'default', got %q", s.Name())
	}
	w := s.Weights()
	if w.VectorWeight != 0.7 {
		t.Fatalf("default VectorWeight: want 0.7, got %v", w.VectorWeight)
	}
}

func TestRankingStrategyByName_FreshnessFirst(t *testing.T) {
	s := RankingStrategyByName("freshness_first")
	if s.Name() != "freshness_first" {
		t.Fatalf("want 'freshness_first', got %q", s.Name())
	}
	w := s.Weights()
	if w.RecencyWeight <= w.VectorWeight {
		t.Fatalf("freshness_first should have RecencyWeight > VectorWeight: recency=%v vector=%v", w.RecencyWeight, w.VectorWeight)
	}
}

func TestRankingStrategyByName_SemanticSearch(t *testing.T) {
	s := RankingStrategyByName("semantic_search")
	w := s.Weights()
	if w.VectorWeight <= w.RecencyWeight {
		t.Fatalf("semantic_search should have VectorWeight > RecencyWeight: vector=%v recency=%v", w.VectorWeight, w.RecencyWeight)
	}
}

func TestRankingStrategyByName_GraphExpansion(t *testing.T) {
	s := RankingStrategyByName("graph_expansion")
	w := s.Weights()
	if w.CentralityWeight <= w.VectorWeight {
		t.Fatalf("graph_expansion should have CentralityWeight > VectorWeight: centrality=%v vector=%v", w.CentralityWeight, w.VectorWeight)
	}
}

func TestRankingStrategyByName_UnknownFallback(t *testing.T) {
	s := RankingStrategyByName("nonexistent")
	if s.Name() != "default" {
		t.Fatalf("unknown strategy should fall back to default, got %q", s.Name())
	}
}

func TestAllStrategies_WeightsAreComplete(t *testing.T) {
	strategies := []RankingStrategy{
		DefaultRanking{}, FreshnessFirst{}, SemanticSearch{}, GraphExpansion{},
	}
	for _, s := range strategies {
		w := s.Weights()
		if w.VectorWeight == 0 && w.RecencyWeight == 0 {
			t.Fatalf("strategy %q has all-zero weights", s.Name())
		}
		if w.DepthPenalty <= 0 {
			t.Fatalf("strategy %q has non-positive DepthPenalty", s.Name())
		}
	}
}

func TestAllStrategies_WithDefaultsIdempotent(t *testing.T) {
	strategies := []RankingStrategy{
		DefaultRanking{}, FreshnessFirst{}, SemanticSearch{}, GraphExpansion{},
	}
	for _, s := range strategies {
		w1 := s.Weights()
		w2 := w1.WithDefaults()
		if w1 != w2 {
			t.Fatalf("strategy %q: WithDefaults changed already-defaulted weights: %+v → %+v", s.Name(), w1, w2)
		}
	}
}
