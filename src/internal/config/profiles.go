package config

import (
	"github.com/pavelveter/hermem/src/internal/core"
)

// RetrievalProfile bundles ranking weights and retrieval tuning
// parameters into a named, reusable profile. Profiles make the
// retrieval engine configurable without changing code.
type RetrievalProfile struct {
	Name              string
	Ranking           core.RankingWeight
	MaxDepth          int
	MaxRetrievedNodes int
	TopK              int
}

// DefaultRetrievalProfile returns the canonical default profile.
func DefaultRetrievalProfile() RetrievalProfile {
	return RetrievalProfile{
		Name:              "default",
		Ranking:           core.RankingWeight{}.WithDefaults(),
		MaxDepth:          2,
		MaxRetrievedNodes: 100,
		TopK:              5,
	}
}

// FreshnessFirstProfile prioritises recently-updated facts.
func FreshnessFirstProfile() RetrievalProfile {
	return RetrievalProfile{
		Name: "freshness_first",
		Ranking: core.RankingWeight{
			VectorWeight:          0.3,
			RecencyWeight:         0.5,
			TemporalWeight:        0.1,
			CentralityWeight:      0.05,
			DepthPenalty:          0.05,
			RecencyHalfLifeHours:  168,
			TemporalHalfLifeHours: 168,
		}.WithDefaults(),
		MaxDepth:          2,
		MaxRetrievedNodes: 100,
		TopK:              5,
	}
}

// SemanticSearchProfile maximises vector similarity.
func SemanticSearchProfile() RetrievalProfile {
	return RetrievalProfile{
		Name: "semantic_search",
		Ranking: core.RankingWeight{
			VectorWeight:          0.85,
			RecencyWeight:         0.05,
			TemporalWeight:        0.02,
			CentralityWeight:      0.03,
			DepthPenalty:          0.05,
			RecencyHalfLifeHours:  1440,
			TemporalHalfLifeHours: 1440,
		}.WithDefaults(),
		MaxDepth:          3,
		MaxRetrievedNodes: 150,
		TopK:              10,
	}
}

// GraphExpansionProfile emphasises graph structure and centrality.
func GraphExpansionProfile() RetrievalProfile {
	return RetrievalProfile{
		Name: "graph_expansion",
		Ranking: core.RankingWeight{
			VectorWeight:          0.3,
			RecencyWeight:         0.1,
			TemporalWeight:        0.05,
			CentralityWeight:      0.45,
			DepthPenalty:          0.1,
			RecencyHalfLifeHours:  720,
			TemporalHalfLifeHours: 720,
		}.WithDefaults(),
		MaxDepth:          4,
		MaxRetrievedNodes: 200,
		TopK:              10,
	}
}

// ConversationMemoryProfile optimises for dialog context retrieval.
func ConversationMemoryProfile() RetrievalProfile {
	return RetrievalProfile{
		Name: "conversation_memory",
		Ranking: core.RankingWeight{
			VectorWeight:          0.5,
			RecencyWeight:         0.35,
			TemporalWeight:        0.1,
			CentralityWeight:      0.0,
			DepthPenalty:          0.05,
			RecencyHalfLifeHours:  24, // 1 day — conversations are fresh
			TemporalHalfLifeHours: 24,
		}.WithDefaults(),
		MaxDepth:          1,
		MaxRetrievedNodes: 50,
		TopK:              5,
	}
}

// RetrievalProfileByName returns the profile matching name, or
// DefaultRetrievalProfile for empty/unknown names.
func RetrievalProfileByName(name string) RetrievalProfile {
	switch name {
	case "freshness_first":
		return FreshnessFirstProfile()
	case "semantic_search":
		return SemanticSearchProfile()
	case "graph_expansion":
		return GraphExpansionProfile()
	case "conversation_memory":
		return ConversationMemoryProfile()
	default:
		return DefaultRetrievalProfile()
	}
}
