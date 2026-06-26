package contradiction

import (
	"context"

	"github.com/pavelveter/hermem/src/internal/core"
)

const llmReasonHit = "LLM contradiction detected"

// LLMChecker is the interface for LLM-based contradiction checking.
// Extracted so the detector is testable without a real LLM.
type LLMChecker interface {
	// IsContradiction returns true if the LLM determines that a and b
	// contradict each other. The confidence score reflects the LLM's
	// certainty (0 = no contradiction, 1 = definite contradiction).
	IsContradiction(ctx context.Context, a, b string) (bool, float32, error)
}

// LLMDetector implements ContradictionDetector using an LLM to
// determine whether two entities contradict each other. This is the
// most accurate but most expensive detector — it should run after
// cheaper lexical and embedding passes in a CompositeDetector pipeline.
type LLMDetector struct {
	checker LLMChecker
}

// NewLLMDetector returns an LLMDetector backed by the given checker.
func NewLLMDetector(checker LLMChecker) *LLMDetector {
	return &LLMDetector{checker: checker}
}

// Detect delegates to the LLM checker. Returns a miss on checker
// errors (defensive — a broken LLM must not block ingestion).
func (d *LLMDetector) Detect(existing, incoming core.Entity) DetectionResult {
	if d.checker == nil {
		return DetectionResult{}
	}
	// Use background context since Detect doesn't receive one.
	ctx := context.Background()
	isContradiction, confidence, err := d.checker.IsContradiction(ctx, existing.Content, incoming.Content)
	if err != nil {
		return DetectionResult{}
	}
	if !isContradiction {
		return DetectionResult{}
	}
	if confidence <= 0 {
		confidence = 0.5
	}
	return DetectionResult{
		Detected:   true,
		Reason:     llmReasonHit,
		Confidence: confidence,
	}
}
