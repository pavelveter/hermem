package contradiction

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

// TestLexicalDetector_Regression locks the full set of known
// contradiction cases through the LexicalDetector to prevent regressions.
func TestLexicalDetector_Regression(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		// English
		{"en_identical", "User likes Go", "User likes Go", false},
		{"en_neg_flip", "User likes Go", "User does not like Go", true},
		{"en_identical_neg", "User does not like Go", "User does not like Go", false},
		{"en_hate_vs_love", "User hates Go", "User loves Go", true},
		{"en_does_not_does", "User does not", "User does", true},

		// Russian
		{"ru_neg_particle", "Я люблю море", "Я не люблю море", true},
		{"ru_hate_to_love", "Я люблю это", "Я ненавижу это", true},
		{"ru_ne_ochen_falls_through", "Я люблю это", "Я не очень люблю это", false},
		{"ru_razlub_inflection", "Я любил это", "Я разлюбил это", true},
		{"ru_substring_falls_through_nravitsya", "Мне нравится это", "Это красиво", false},
		{"ru_nikogda_neg", "Хочу туда поехать", "Никогда не хочу туда ехать", true},
		{"ru_identical", "Я люблю это", "Я люблю это", false},
		{"ru_neg_identical", "Я не люблю это", "Я не люблю это", false},
		{"ru_double_neg_vs_plain_neg", "Я не ненавижу это", "Я ненавижу это", true},
		{"ru_cross_lang_detect", "User loves X", "User не любит X", true},
		{"ru_stemmer_lubit_not_lubit", "Я люблю это", "Я не люблю это", true},
		{"ru_stemmer_lubit_ne_lubit", "Я любит море", "Я не любит море", true},
		{"ru_stemmer_polubil_ne_polubil", "Я полюбил это", "Я не полюбил это", true},
		{"ru_stemmer_lubit_labila_no_neg", "Я любит это", "Я любила это", false},
	}
	detector := NewLexicalDetector()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := detector.Detect(core.Entity{Content: c.a}, core.Entity{Content: c.b})
			if result.Detected != c.want {
				t.Errorf("Detect(%q, %q) = %v, want %v", c.a, c.b, result.Detected, c.want)
			}
		})
	}
}

// TestEmbeddingDetector_Regression locks known embedding-based
// contradiction cases.
func TestEmbeddingDetector_Regression(t *testing.T) {
	cases := []struct {
		name      string
		a         core.Entity
		b         core.Entity
		threshold float32
		want      bool
	}{
		{
			name:      "identical_content_same_emb_no_hit",
			a:         core.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			b:         core.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			threshold: 0.8,
			want:      false,
		},
		{
			name:      "similar_emb_different_content_hit",
			a:         core.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			b:         core.Entity{Content: "Go is slow", Embedding: []float32{0.95, 0.31, 0}},
			threshold: 0.8,
			want:      true,
		},
		{
			name:      "orthogonal_emb_no_hit",
			a:         core.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			b:         core.Entity{Content: "Go is slow", Embedding: []float32{0, 1, 0}},
			threshold: 0.8,
			want:      false,
		},
	}
	detector := NewEmbeddingDetector(0.8)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := NewEmbeddingDetector(c.threshold)
			_ = detector // ensure original still works
			result := d.Detect(c.a, c.b)
			if result.Detected != c.want {
				t.Errorf("Detect() = %v, want %v", result.Detected, c.want)
			}
		})
	}
}

// TestCompositeDetector_PipelineRegression tests the full composite
// pipeline with lexical + embedding detectors in order.
func TestCompositeDetector_PipelineRegression(t *testing.T) {
	lexical := NewLexicalDetector()
	embedding := NewEmbeddingDetector(0.8)
	pipeline := NewCompositeDetector(lexical, embedding)

	cases := []struct {
		name string
		a, b core.Entity
		want bool
	}{
		{
			name: "lexical_catches_negation",
			a:    core.Entity{Content: "Я люблю море"},
			b:    core.Entity{Content: "Я не люблю море"},
			want: true,
		},
		{
			name: "embedding_catches_semantic",
			a:    core.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			b:    core.Entity{Content: "Go is slow", Embedding: []float32{0.95, 0.31, 0}},
			want: true,
		},
		{
			name: "no_hit_on_identical",
			a:    core.Entity{Content: "Go is fast"},
			b:    core.Entity{Content: "Go is fast"},
			want: false,
		},
		{
			name: "no_hit_on_different_topic",
			a:    core.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			b:    core.Entity{Content: "Python is slow", Embedding: []float32{0, 1, 0}},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := pipeline.Detect(c.a, c.b)
			if result.Detected != c.want {
				t.Errorf("pipeline.Detect() = %v (reason=%q), want %v", result.Detected, result.Reason, c.want)
			}
		})
	}
}

// TestLLMDetector_Regression verifies the LLM detector contract
// through its mock interface.
func TestLLMDetector_Regression(t *testing.T) {
	// A mock that detects contradictions on specific keywords.
	mock := &mockLLMChecker{
		contradicts: true,
		confidence:  0.85,
	}
	detector := NewLLMDetector(mock)

	// Should detect and record one call.
	result := detector.Detect(
		core.Entity{Content: "Go is great"},
		core.Entity{Content: "Go is terrible"},
	)
	if !result.Detected {
		t.Error("expected detection")
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 call, got %d", mock.calls)
	}

	// Error path — should return miss.
	mock.err = context.DeadlineExceeded
	mock.calls = 0
	result = detector.Detect(
		core.Entity{Content: "a"},
		core.Entity{Content: "b"},
	)
	if result.Detected {
		t.Error("expected miss on error")
	}
}
