package retrieval

import "github.com/pavelveter/hermem/src/internal/core"

// RankingStrategy defines a named ranking policy that produces
// RankingWeight parameters and optional score modifiers for the
// retrieval engine. Implementations encapsulate the "philosophy"
// of ranking (what matters most) without mutating shared state.
type RankingStrategy interface {
	// Name returns a short identifier for logging/config (e.g. "default", "freshness_first").
	Name() string
	// Weights returns the ranking weights for this strategy.
	Weights() core.RankingWeight
}

// DefaultRanking uses the canonical weights from RankingWeight.WithDefaults().
type DefaultRanking struct{}

func (DefaultRanking) Name() string                { return "default" }
func (DefaultRanking) Weights() core.RankingWeight { return core.RankingWeight{}.WithDefaults() }

// FreshnessFirst prioritizes recency over vector similarity.
type FreshnessFirst struct{}

func (FreshnessFirst) Name() string { return "freshness_first" }
func (FreshnessFirst) Weights() core.RankingWeight {
	return core.RankingWeight{
		VectorWeight:          0.3,
		RecencyWeight:         0.5,
		TemporalWeight:        0.1,
		CentralityWeight:      0.05,
		DepthPenalty:          0.05,
		RecencyHalfLifeHours:  168, // 7 days
		TemporalHalfLifeHours: 168,
	}.WithDefaults()
}

// SemanticSearch prioritizes vector similarity.
type SemanticSearch struct{}

func (SemanticSearch) Name() string { return "semantic_search" }
func (SemanticSearch) Weights() core.RankingWeight {
	return core.RankingWeight{
		VectorWeight:          0.85,
		RecencyWeight:         0.05,
		TemporalWeight:        0.02,
		CentralityWeight:      0.03,
		DepthPenalty:          0.05,
		RecencyHalfLifeHours:  1440, // 60 days
		TemporalHalfLifeHours: 1440,
	}.WithDefaults()
}

// GraphExpansion prioritizes graph structure and centrality.
type GraphExpansion struct{}

func (GraphExpansion) Name() string { return "graph_expansion" }
func (GraphExpansion) Weights() core.RankingWeight {
	return core.RankingWeight{
		VectorWeight:          0.3,
		RecencyWeight:         0.1,
		TemporalWeight:        0.05,
		CentralityWeight:      0.45,
		DepthPenalty:          0.1,
		RecencyHalfLifeHours:  720,
		TemporalHalfLifeHours: 720,
	}.WithDefaults()
}

// RankingStrategyByName returns the strategy matching the given name,
// or DefaultRanking for empty/unknown names.
func RankingStrategyByName(name string) RankingStrategy {
	switch name {
	case "freshness_first":
		return FreshnessFirst{}
	case "semantic_search":
		return SemanticSearch{}
	case "graph_expansion":
		return GraphExpansion{}
	default:
		return DefaultRanking{}
	}
}
