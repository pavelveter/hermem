package retrieval

import (
	"testing"
)

func TestLinearNormalizer_MidpointIsHalf(t *testing.T) {
	n := LinearNormalizer{Min: 0, Max: 10}
	got := n.Normalize(5)
	if !floatNear(got, 0.5) {
		t.Fatalf("linear midpoint: want 0.5, got %v", got)
	}
}

func TestLinearNormalizer_Clamps(t *testing.T) {
	n := LinearNormalizer{Min: 0, Max: 10}
	if got := n.Normalize(-1); got != 0 {
		t.Fatalf("below min: want 0, got %v", got)
	}
	if got := n.Normalize(11); got != 1 {
		t.Fatalf("above max: want 1, got %v", got)
	}
}

func TestLogNormalizer_ZeroIsZero(t *testing.T) {
	n := LogNormalizer{Max: 10}
	if got := n.Normalize(0); got != 0 {
		t.Fatalf("zero: want 0, got %v", got)
	}
}

func TestLogNormalizer_ScalesWithMax(t *testing.T) {
	n := LogNormalizer{Max: 100}
	got := n.Normalize(50)
	if got <= 0 || got >= 1 {
		t.Fatalf("log normalize 50/100: want in (0,1), got %v", got)
	}
}

func TestSigmoidNormalizer_MidpointIsHalf(t *testing.T) {
	n := SigmoidNormalizer{Midpoint: 5, Steepness: 1}
	got := n.Normalize(5)
	if !floatNear(got, 0.5) {
		t.Fatalf("sigmoid midpoint: want 0.5, got %v", got)
	}
}

func TestTanhNormalizer_MidpointIsHalf(t *testing.T) {
	n := TanhNormalizer{Midpoint: 5, Steepness: 1}
	got := n.Normalize(5)
	if !floatNear(got, 0.5) {
		t.Fatalf("tanh midpoint: want 0.5, got %v", got)
	}
}

func TestNormalizerByName_DefaultIsLinear(t *testing.T) {
	n := NormalizerByName("", 0, 1)
	if _, ok := n.(LinearNormalizer); !ok {
		t.Fatalf("default should be LinearNormalizer, got %T", n)
	}
}

func TestAllNormalizers_OutputInRange(t *testing.T) {
	inputs := []float32{-1, 0, 0.25, 0.5, 0.75, 1.0, 2.0}
	normalizers := []ScoreNormalizer{
		LinearNormalizer{Min: 0, Max: 1},
		LogNormalizer{Max: 1},
		SigmoidNormalizer{Midpoint: 0.5, Steepness: 8},
		TanhNormalizer{Midpoint: 0.5, Steepness: 4},
	}
	for _, n := range normalizers {
		for _, raw := range inputs {
			got := n.Normalize(raw)
			if got < -0.001 || got > 1.001 {
				t.Fatalf("%T: Normalize(%v) = %v not in [0,1]", n, raw, got)
			}
		}
	}
}
