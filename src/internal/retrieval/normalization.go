package retrieval

import "math"

// ScoreNormalizer transforms a raw feature value into [0, 1].
// Implementations must be pure functions with no side effects.
type ScoreNormalizer interface {
	// Normalize maps raw to [0, 1]. Out-of-range inputs are clamped.
	Normalize(raw float32) float32
}

// LinearNormalizer scales raw linearly from [min, max] to [0, 1].
// The current default — simple, fast, easy to reason about.
type LinearNormalizer struct {
	Min, Max float32
}

func (l LinearNormalizer) Normalize(raw float32) float32 {
	if l.Max <= l.Min {
		return 0
	}
	v := (raw - l.Min) / (l.Max - l.Min)
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// LogNormalizer applies log scaling: log(1 + raw) / log(1 + max).
// Good for features with heavy-tailed distributions (e.g. degree).
type LogNormalizer struct {
	Max float32
}

func (l LogNormalizer) Normalize(raw float32) float32 {
	if raw <= 0 {
		return 0
	}
	if l.Max <= 0 {
		return 0
	}
	v := float32(math.Log1p(float64(raw)) / math.Log1p(float64(l.Max)))
	if v > 1 {
		return 1
	}
	return v
}

// SigmoidNormalizer applies the sigmoid function centered at midpoint.
// Produces an S-shaped curve — useful when extreme values should be
// compressed but the transition region should be sensitive.
type SigmoidNormalizer struct {
	Midpoint float32 // center of the S-curve
	Steepness float32 // controls the slope
}

func (s SigmoidNormalizer) Normalize(raw float32) float32 {
	if s.Steepness == 0 {
		s.Steepness = 1.0
	}
	if s.Midpoint == 0 {
		s.Midpoint = 0.5
	}
	x := float64(s.Steepness) * float64(raw-s.Midpoint)
	return float32(1.0 / (1.0 + math.Exp(-x)))
}

// TanhNormalizer applies tanh scaling: output = (tanh(k*(raw-mid)) + 1) / 2.
// Similar to sigmoid but bounded more tightly around the midpoint.
type TanhNormalizer struct {
	Midpoint  float32
	Steepness float32
}

func (t TanhNormalizer) Normalize(raw float32) float32 {
	if t.Steepness == 0 {
		t.Steepness = 1.0
	}
	x := float64(t.Steepness) * float64(raw-t.Midpoint)
	return float32((math.Tanh(x) + 1.0) / 2.0)
}

// NormalizerByName returns the named normalizer, or LinearNormalizer{Min:0, Max:1}.
func NormalizerByName(name string, min, max float32) ScoreNormalizer {
	switch name {
	case "log":
		return LogNormalizer{Max: max}
	case "sigmoid":
		return SigmoidNormalizer{Midpoint: (min + max) / 2, Steepness: 4.0 / (max - min)}
	case "tanh":
		return TanhNormalizer{Midpoint: (min + max) / 2, Steepness: 4.0 / (max - min)}
	default:
		return LinearNormalizer{Min: min, Max: max}
	}
}
