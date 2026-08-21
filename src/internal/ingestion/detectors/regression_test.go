package detectors

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/pkg/domain"
)

func TestLexicalDetector_Regression(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"en_identical", "User likes Go", "User likes Go", false},
		{"en_neg_flip", "User likes Go", "User does not like Go", true},
		{"en_identical_neg", "User does not like Go", "User does not like Go", false},
		{"en_hate_vs_love", "User hates Go", "User loves Go", true},
		{"en_does_not_does", "User does not", "User does", true},

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
			result := detector.Detect(domain.Entity{Content: c.a}, domain.Entity{Content: c.b})
			if result.Detected != c.want {
				t.Errorf("Detect(%q, %q) = %v, want %v", c.a, c.b, result.Detected, c.want)
			}
		})
	}
}

func TestEmbeddingDetector_Regression(t *testing.T) {
	cases := []struct {
		name      string
		a         domain.Entity
		b         domain.Entity
		threshold float32
		want      bool
	}{
		{
			name:      "identical_content_same_emb_no_hit",
			a:         domain.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			b:         domain.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			threshold: 0.8,
			want:      false,
		},
		{
			name:      "similar_emb_different_content_hit",
			a:         domain.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			b:         domain.Entity{Content: "Go is slow", Embedding: []float32{0.95, 0.31, 0}},
			threshold: 0.8,
			want:      true,
		},
		{
			name:      "orthogonal_emb_no_hit",
			a:         domain.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			b:         domain.Entity{Content: "Go is slow", Embedding: []float32{0, 1, 0}},
			threshold: 0.8,
			want:      false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := NewEmbeddingDetector(c.threshold)
			result := d.Detect(c.a, c.b)
			if result.Detected != c.want {
				t.Errorf("Detect() = %v, want %v", result.Detected, c.want)
			}
		})
	}
}

func TestCompositeDetector_PipelineRegression(t *testing.T) {
	lexical := NewLexicalDetector()
	embedding := NewEmbeddingDetector(0.8)
	pipeline := NewCompositeDetector(lexical, embedding)

	cases := []struct {
		name string
		a, b domain.Entity
		want bool
	}{
		{
			name: "lexical_catches_negation",
			a:    domain.Entity{Content: "Я люблю море", Embedding: []float32{1, 0, 0}},
			b:    domain.Entity{Content: "Я не люблю море", Embedding: []float32{0.95, 0.31, 0}},
			want: true,
		},
		{
			name: "embedding_catches_semantic",
			a:    domain.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			b:    domain.Entity{Content: "Go is slow", Embedding: []float32{0.95, 0.31, 0}},
			want: true,
		},
		{
			name: "no_hit_on_identical",
			a:    domain.Entity{Content: "Go is fast"},
			b:    domain.Entity{Content: "Go is fast"},
			want: false,
		},
		{
			name: "no_hit_on_different_topic",
			a:    domain.Entity{Content: "Go is fast", Embedding: []float32{1, 0, 0}},
			b:    domain.Entity{Content: "Python is slow", Embedding: []float32{0, 1, 0}},
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

func TestLLMDetector_Regression(t *testing.T) {
	mock := &mockLLMChecker{
		contradicts: true,
		confidence:  0.85,
	}
	detector := NewLLMDetector(mock)

	result := detector.Detect(
		domain.Entity{Content: "Go is great"},
		domain.Entity{Content: "Go is terrible"},
	)
	if !result.Detected {
		t.Error("expected detection")
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 call, got %d", mock.calls)
	}

	mock.err = context.DeadlineExceeded
	mock.calls = 0
	result = detector.Detect(
		domain.Entity{Content: "a"},
		domain.Entity{Content: "b"},
	)
	if result.Detected {
		t.Error("expected miss on error")
	}
}
