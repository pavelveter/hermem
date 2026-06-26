package detectors

import (
	"math"

	"github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
)

const embeddingReasonHit = "embedding similarity contradiction"

// EmbeddingDetector implements ContradictionDetector using cosine
// similarity between entity embeddings.
type EmbeddingDetector struct {
	Threshold float32
}

// NewEmbeddingDetector returns an EmbeddingDetector with the given
// similarity threshold.
func NewEmbeddingDetector(threshold float32) *EmbeddingDetector {
	if threshold <= 0 {
		threshold = 0.8
	}
	return &EmbeddingDetector{Threshold: threshold}
}

// Detect checks whether existing and incoming entities are semantically
// similar but textually divergent, indicating a potential contradiction.
func (d *EmbeddingDetector) Detect(existing, incoming core.Entity) contradiction.DetectionResult {
	if len(existing.Embedding) == 0 || len(incoming.Embedding) == 0 {
		return contradiction.DetectionResult{}
	}
	if len(existing.Embedding) != len(incoming.Embedding) {
		return contradiction.DetectionResult{}
	}

	sim := cosineSimilarity(existing.Embedding, incoming.Embedding)
	if sim < d.Threshold {
		return contradiction.DetectionResult{}
	}

	if existing.Content == incoming.Content {
		return contradiction.DetectionResult{}
	}

	return contradiction.DetectionResult{
		Detected:   true,
		Reason:     embeddingReasonHit,
		Confidence: sim,
	}
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(normA)*float64(normB)))
}
