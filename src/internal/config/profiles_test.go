package config

import (
	"testing"
)

func TestRetrievalProfileByName_Default(t *testing.T) {
	p := RetrievalProfileByName("")
	if p.Name != "default" {
		t.Fatalf("want 'default', got %q", p.Name)
	}
	if p.MaxDepth <= 0 {
		t.Fatalf("default MaxDepth should be > 0, got %d", p.MaxDepth)
	}
}

func TestRetrievalProfileByName_AllKnown(t *testing.T) {
	names := []string{"default", "freshness_first", "semantic_search", "graph_expansion", "conversation_memory"}
	seen := make(map[string]bool)
	for _, name := range names {
		p := RetrievalProfileByName(name)
		if seen[p.Name] {
			t.Fatalf("duplicate profile name: %q", p.Name)
		}
		seen[p.Name] = true
		if p.Ranking.VectorWeight == 0 && p.Ranking.RecencyWeight == 0 {
			t.Fatalf("profile %q has all-zero weights", p.Name)
		}
	}
}

func TestRetrievalProfileByName_UnknownFallback(t *testing.T) {
	p := RetrievalProfileByName("nonexistent")
	if p.Name != "default" {
		t.Fatalf("unknown profile should fall back to default, got %q", p.Name)
	}
}

func TestAllProfiles_WeightsDefaultable(t *testing.T) {
	profiles := []RetrievalProfile{
		DefaultRetrievalProfile(), FreshnessFirstProfile(), SemanticSearchProfile(),
		GraphExpansionProfile(), ConversationMemoryProfile(),
	}
	for _, p := range profiles {
		w := p.Ranking.WithDefaults()
		if w.VectorWeight == 0 {
			t.Fatalf("profile %q: VectorWeight still 0 after WithDefaults", p.Name)
		}
	}
}
