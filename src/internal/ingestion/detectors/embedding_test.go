package detectors

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

func TestEmbeddingDetector(t *testing.T) {
	cases := []struct {
		name      string
		existing  core.Entity
		incoming  core.Entity
		threshold float32
		want      bool
	}{
		{
			name:      "identical_embeddings_same_content_no_hit",
			existing:  core.Entity{Content: "Go is great", Embedding: []float32{1, 0, 0}},
			incoming:  core.Entity{Content: "Go is great", Embedding: []float32{1, 0, 0}},
			threshold: 0.8,
			want:      false,
		},
		{
			name:      "similar_embeddings_different_content_hit",
			existing:  core.Entity{Content: "Go is great", Embedding: []float32{1, 0, 0}},
			incoming:  core.Entity{Content: "Go is terrible", Embedding: []float32{0.95, 0.31, 0}},
			threshold: 0.8,
			want:      true,
		},
		{
			name:      "low_similarity_no_hit",
			existing:  core.Entity{Content: "Go is great", Embedding: []float32{1, 0, 0}},
			incoming:  core.Entity{Content: "Python is great", Embedding: []float32{0, 1, 0}},
			threshold: 0.8,
			want:      false,
		},
		{
			name:      "missing_embedding_existing",
			existing:  core.Entity{Content: "Go is great"},
			incoming:  core.Entity{Content: "Go is terrible", Embedding: []float32{1, 0, 0}},
			threshold: 0.8,
			want:      false,
		},
		{
			name:      "missing_embedding_incoming",
			existing:  core.Entity{Content: "Go is great", Embedding: []float32{1, 0, 0}},
			incoming:  core.Entity{Content: "Go is terrible"},
			threshold: 0.8,
			want:      false,
		},
		{
			name:      "both_missing_embeddings",
			existing:  core.Entity{Content: "Go is great"},
			incoming:  core.Entity{Content: "Go is terrible"},
			threshold: 0.8,
			want:      false,
		},
		{
			name:      "dimension_mismatch",
			existing:  core.Entity{Content: "Go is great", Embedding: []float32{1, 0}},
			incoming:  core.Entity{Content: "Go is terrible", Embedding: []float32{1, 0, 0}},
			threshold: 0.8,
			want:      false,
		},
		{
			name:      "zero_vector_no_hit",
			existing:  core.Entity{Content: "Go is great", Embedding: []float32{0, 0, 0}},
			incoming:  core.Entity{Content: "Go is terrible", Embedding: []float32{1, 0, 0}},
			threshold: 0.8,
			want:      false,
		},
		{
			name:      "default_threshold_when_zero",
			existing:  core.Entity{Content: "a", Embedding: []float32{1, 0, 0}},
			incoming:  core.Entity{Content: "b", Embedding: []float32{0.99, 0.14, 0}},
			threshold: 0,
			want:      true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := NewEmbeddingDetector(c.threshold)
			result := d.Detect(c.existing, c.incoming)
			if result.Detected != c.want {
				t.Errorf("Detect() detected=%v, want %v (reason=%q, confidence=%v)",
					result.Detected, c.want, result.Reason, result.Confidence)
			}
			if result.Detected && result.Reason == "" {
				t.Error("hit but reason empty")
			}
			if !result.Detected && result.Reason != "" {
				t.Errorf("miss but reason non-empty (%q)", result.Reason)
			}
			if result.Detected && result.Confidence <= 0 {
				t.Errorf("hit but confidence=%v; want > 0", result.Confidence)
			}
		})
	}
}

func TestCosineSimilarity(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0},
		{"empty", nil, nil, 0},
		{"mismatched_len", []float32{1}, []float32{1, 0}, 0},
		{"zero_vector", []float32{0, 0}, []float32{1, 0}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cosineSimilarity(c.a, c.b)
			if got != c.want {
				t.Errorf("cosineSimilarity = %v, want %v", got, c.want)
			}
		})
	}
}
