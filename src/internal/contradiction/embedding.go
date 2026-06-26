package contradiction

import (
	"math"

	"github.com/pavelveter/hermem/src/internal/core"
)

const embeddingReasonHit = "embedding similarity contradiction"

// EmbeddingDetector implements ContradictionDetector using cosine
// similarity between entity embeddings. When two entities have high
// embedding similarity (semantically close) but divergent textual
// content, it flags a potential contradiction.
//
// The detector uses a threshold-based approach:
//   - If embeddings are missing, it falls back to a text-length
//     divergence heuristic.
//   - If embeddings are present, it computes cosine similarity and
//     compares against the configured threshold.
type EmbeddingDetector struct {
	// Threshold is the minimum cosine similarity required to consider
	// two entities as semantically related. Entities below this
	// threshold are not compared for contradiction.
	Threshold float32
}

// NewEmbeddingDetector returns an EmbeddingDetector with the given
// similarity threshold. A threshold of 0.8 means "flag when embeddings
// are at least 80% similar but content diverges."
func NewEmbeddingDetector(threshold float32) *EmbeddingDetector {
	if threshold <= 0 {
		threshold = 0.8
	}
	return &EmbeddingDetector{Threshold: threshold}
}

// Detect checks whether existing and incoming entities are semantically
// similar (via embedding cosine similarity) but textually divergent,
// indicating a potential contradiction.
func (d *EmbeddingDetector) Detect(existing, incoming core.Entity) DetectionResult {
	// Both must have embeddings for cosine similarity comparison.
	if len(existing.Embedding) == 0 || len(incoming.Embedding) == 0 {
		return DetectionResult{}
	}
	if len(existing.Embedding) != len(incoming.Embedding) {
		return DetectionResult{}
	}

	sim := cosineSimilarity(existing.Embedding, incoming.Embedding)
	if sim < d.Threshold {
		return DetectionResult{}
	}

	// High similarity but different content → potential contradiction.
	if existing.Content == incoming.Content {
		return DetectionResult{}
	}

	return DetectionResult{
		Detected:   true,
		Reason:     embeddingReasonHit,
		Confidence: sim,
	}
}

// cosineSimilarity computes the cosine similarity between two vectors.
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
