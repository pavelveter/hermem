package evolution

import (
	"math"
	"time"
)

// TrustWeights configures the trust-scoring formula.
type TrustWeights struct {
	// SourceTrust maps source_kind → trust weight in [0,1].
	// Unknown source kinds default to 0.5.
	SourceTrust map[string]float64
	// RecencyHalfLifeHours controls the exponential decay of recency
	// (default 720 = 30 days). Older beliefs get lower recency.
	RecencyHalfLifeHours float64
}

// TrustDefaults returns TrustWeights with sensible defaults.
func TrustDefaults() TrustWeights {
	return TrustWeights{
		SourceTrust: map[string]float64{
			"user":        1.0,
			"observation": 0.9,
			"extraction":  0.7,
			"inference":   0.5,
			"external":    0.3,
		},
		RecencyHalfLifeHours: 720,
	}
}

// TrustScore computes a composite trust score for a belief.
//
// Formula: trust = confidence * sourceWeight * recencyFactor
//
// where recencyFactor = exp(-hoursSinceUpdate / halfLife).
// Beliefs updated more recently approach 1.0; beliefs never touched
// (zero UpdatedAt) get recencyFactor = 1 (as fresh as possible).
func TrustScore(confidence float64, sourceKind string, updatedAt time.Time, w TrustWeights) float64 {
	sw, ok := w.SourceTrust[sourceKind]
	if !ok {
		sw = 0.5
	}
	hl := w.RecencyHalfLifeHours
	if hl <= 0 {
		hl = 720
	}

	var rf float64
	if updatedAt.IsZero() {
		rf = 1.0
	} else {
		hours := time.Since(updatedAt.UTC()).Hours()
		if hours <= 0 {
			rf = 1.0
		} else {
			rf = math.Exp(-hours / hl)
		}
	}

	return clamp(confidence*sw*rf, 0, 1)
}
