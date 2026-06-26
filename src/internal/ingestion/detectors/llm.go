package detectors

import (
	"context"

	"github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
)

const llmReasonHit = "LLM contradiction detected"

// LLMChecker is the interface for LLM-based contradiction checking.
type LLMChecker interface {
	IsContradiction(ctx context.Context, a, b string) (bool, float32, error)
}

// LLMDetector implements ContradictionDetector using an LLM to
// determine whether two entities contradict each other.
type LLMDetector struct {
	checker LLMChecker
}

// NewLLMDetector returns an LLMDetector backed by the given checker.
func NewLLMDetector(checker LLMChecker) *LLMDetector {
	return &LLMDetector{checker: checker}
}

// Detect delegates to the LLM checker. Returns a miss on checker errors.
func (d *LLMDetector) Detect(existing, incoming core.Entity) contradiction.DetectionResult {
	if d.checker == nil {
		return contradiction.DetectionResult{}
	}
	ctx := context.Background()
	isContradiction, confidence, err := d.checker.IsContradiction(ctx, existing.Content, incoming.Content)
	if err != nil {
		return contradiction.DetectionResult{}
	}
	if !isContradiction {
		return contradiction.DetectionResult{}
	}
	if confidence <= 0 {
		confidence = 0.5
	}
	return contradiction.DetectionResult{
		Detected:   true,
		Reason:     llmReasonHit,
		Confidence: confidence,
	}
}
