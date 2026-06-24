//go:build darwin && cgo

package vector

import (
	"math"
	"testing"
)

// --- cgo parity tests: cblas-driven answers must agree with the pure-Go fallback ---
//
// Tolerance is generous (1e-4 instead of the 1e-5 used by cosine_test.go for pure-Go)
// because cblas_sdot uses FMA on M-series silicon and the accumulator order differs
// from Go's plain scalar loop. For 768-dim embeddings this matches what Apple's
// vForge ships as the "single-precision BLAS reference baseline".

const cgoEps = float32(1e-4)

func floatNearCG(t *testing.T, name string, got, want float32) {
	t.Helper()
	if math.IsNaN(float64(got)) || math.IsNaN(float64(want)) {
		t.Fatalf("%s: NaN (got=%v want=%v)", name, got, want)
	}
	if math.Abs(float64(got-want)) > float64(cgoEps) {
		t.Fatalf("%s: |got - want| = %v > eps=%v (got=%v want=%v)",
			name, math.Abs(float64(got-want)), cgoEps, got, want)
	}
}

func TestCGOVectorNorm_RealisticDim(t *testing.T) {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(i+1) * 0.01
	}
	manual := pureGoVectorNorm(v)
	cgo := VectorNorm(v)
	floatNearCG(t, "VectorNorm(768)", cgo, manual)
}

func TestCGONormalizeVector_UnitLength(t *testing.T) {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(i+1) * 0.01
	}
	cgo := append([]float32(nil), v...)
	NormalizeVector(cgo)
	// After normalization, norm must be 1.0 within simd drift.
	if math.Abs(float64(VectorNorm(cgo)-1.0)) > 1e-4 {
		t.Fatalf("post-normalize norm != 1: %v", VectorNorm(cgo))
	}
}

func TestCGOCosineSimilarity_RealisticDim(t *testing.T) {
	a := make([]float32, 768)
	b := make([]float32, 768)
	for i := range a {
		a[i] = float32(i+1) * 0.001
		b[i] = float32(768-i) * 0.001
	}
	manual := pureGoCosineSimilarity(a, b)
	cgo := CosineSimilarity(a, b)
	floatNearCG(t, "CosineSimilarity(768)", cgo, manual)
}

func TestCGOCosineSimilarityWithNorm_UsesPrecomputed(t *testing.T) {
	a := make([]float32, 768)
	b := make([]float32, 768)
	for i := range a {
		a[i] = float32(i+1) * 0.001
		b[i] = float32(i+1) * 0.002
	}
	bNorm := VectorNorm(b) // the "precomputed norm" path
	manual := pureGoCosineSimilarityWithNorm(a, b, bNorm)
	cgo := CosineSimilarityWithNorm(a, b, bNorm)
	floatNearCG(t, "CosineSimilarityWithNorm(768)", cgo, manual)
}

func TestCGOBatchDotProducts_RealisticShape(t *testing.T) {
	const rows, cols = 1024, 768
	q := make([]float32, cols)
	for i := range q {
		q[i] = float32(i+1) * 0.001
	}
	m := make([]float32, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			m[r*cols+c] = float32(r+c+1) * 0.0001
		}
	}
	cgoDots := make([]float32, rows)
	BatchDotProducts(q, m, rows, cols, cgoDots)

	// Reference: pure-Go loop.
	refDots := make([]float32, rows)
	pureGoBatchDot(q, m, rows, cols, refDots)

	maxDelta := float32(0)
	for i := range cgoDots {
		d := cgoDots[i] - refDots[i]
		if d < 0 {
			d = -d
		}
		if d > maxDelta {
			maxDelta = d
		}
	}
	// 1024 × 768 FMA drift: magnitudes can reach ~50, so an absolute
	// 5e-4 tolerance (≈1e-5 relative) is comfortably above FMA noise without
	// admitting real algorithmic divergence.
	if maxDelta > 5e-4 {
		t.Fatalf("BatchDotProducts cgo vs pure-Go max delta = %v (>5e-4) on %dx%d", maxDelta, rows, cols)
	}
}

func TestCGOBatchDotProducts_ZeroRows(t *testing.T) {
	// rows==0 must be a clean no-op (matches pure-Go behavior — outer loop
	// never executes).
	BatchDotProducts([]float32{1, 2, 3}, nil, 0, 3, []float32{})
}

func TestCGOBatchDotProducts_QueryShorterThanCols_Panics(t *testing.T) {
	// Bad input must panic loudly from the bounds-bump on query[cols-1],
	// matching pure-Go's panic-on-bad-input behavior in cosine.go so callers
	// surface the bug instead of silently reading garbage.
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("query shorter than cols must panic")
		}
	}()
	BatchDotProducts([]float32{1, 2}, []float32{1, 2, 3, 4}, 1, 4, []float32{0})
}

func TestCGOBatchDotProducts_DotShorterThanRows_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("dot shorter than rows must panic")
		}
	}()
	BatchDotProducts([]float32{1, 2, 3}, []float32{1, 2, 3, 4, 5, 6}, 2, 3, []float32{0})
}

// --- pure-Go reference copies (test-local). Kept tiny so the test file stays self-contained. ---

func pureGoVectorNorm(v []float32) float32 {
	var s float32
	for _, x := range v {
		s += x * x
	}
	return float32(math.Sqrt(float64(s)))
}

func pureGoCosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, nA, nB float32
	for i := range a {
		dot += a[i] * b[i]
		nA += a[i] * a[i]
		nB += b[i] * b[i]
	}
	if nA == 0 || nB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(nA))) * float32(math.Sqrt(float64(nB))))
}

func pureGoCosineSimilarityWithNorm(a, b []float32, normB float32) float32 {
	if len(a) != len(b) || len(a) == 0 || normB == 0 {
		return 0
	}
	var dot, nA float32
	for i := range a {
		dot += a[i] * b[i]
		nA += a[i] * a[i]
	}
	if nA == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(nA))) * normB)
}

func pureGoBatchDot(q []float32, m []float32, rows, cols int, dot []float32) {
	for r := 0; r < rows; r++ {
		var d float32
		for c := 0; c < cols; c++ {
			d += q[c] * m[r*cols+c]
		}
		dot[r] = d
	}
}
