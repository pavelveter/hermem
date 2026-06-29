package retrieval

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

func TestCountTokens(t *testing.T) {
	cases := []struct {
		input string
		min   int
		max   int
	}{
		{"", 0, 0},
		{"hello", 1, 2},
		{"hello world foo bar", 4, 6},
		{"This is a longer sentence with many words to test token estimation", 12, 20},
	}
	for _, c := range cases {
		tokens := CountTokens(c.input)
		if tokens < c.min || tokens > c.max {
			t.Errorf("CountTokens(%q) = %d, want [%d, %d]", c.input, tokens, c.min, c.max)
		}
	}
}

func TestTrimByTokenBudget(t *testing.T) {
	t.Run("nil result unchanged", func(t *testing.T) {
		result := TrimByTokenBudget(nil, 100)
		if result != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("zero budget unchanged", func(t *testing.T) {
		result := &core.RetrievalResult{
			SeedNodes:  []core.GraphNode{{Entity: core.Entity{ID: "a"}}},
			WorldFacts: []core.RetrievedFact{{Content: "fact one"}, {Content: "fact two"}},
		}
		out := TrimByTokenBudget(result, 0)
		if len(out.WorldFacts) != 2 {
			t.Fatalf("expected 2 facts, got %d", len(out.WorldFacts))
		}
	})

	t.Run("budget trims facts", func(t *testing.T) {
		facts := make([]core.RetrievedFact, 20)
		for i := range facts {
			facts[i] = core.RetrievedFact{Content: "This is a test fact with enough content to consume tokens"}
		}
		result := &core.RetrievalResult{
			SeedNodes:  []core.GraphNode{{Entity: core.Entity{ID: "a"}}},
			WorldFacts: facts,
		}
		out := TrimByTokenBudget(result, 50)
		if len(out.WorldFacts) >= 20 {
			t.Fatal("expected trimming")
		}
		if len(out.WorldFacts) == 0 {
			t.Fatal("expected at least some facts")
		}
	})

	t.Run("large budget keeps all", func(t *testing.T) {
		result := &core.RetrievalResult{
			SeedNodes:  []core.GraphNode{{Entity: core.Entity{ID: "a"}}},
			WorldFacts: []core.RetrievedFact{{Content: "short"}},
		}
		out := TrimByTokenBudget(result, 100000)
		if len(out.WorldFacts) != 1 {
			t.Fatal("expected all facts kept")
		}
	})
}

func TestTokenEstimate(t *testing.T) {
	facts := []struct{ Content string }{
		{Content: "hello"},
		{Content: "world"},
	}
	tokens := TokenEstimate(facts)
	if tokens < 4 {
		t.Fatalf("expected at least 4 tokens, got %d", tokens)
	}
}
