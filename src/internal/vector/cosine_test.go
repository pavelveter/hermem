package vector

import (
	"math"
	"testing"
)

const eps = float32(1e-5)

func floatNear(a, b float32) bool {
	if math.IsNaN(float64(a)) || math.IsNaN(float64(b)) {
		return false
	}
	return math.Abs(float64(a-b)) < float64(eps)
}

// --- VectorNorm ---

func TestVectorNorm_AllZeros(t *testing.T) {
	if got := VectorNorm([]float32{0, 0, 0}); got != 0 {
		t.Fatalf("want 0, got %v", got)
	}
}

func TestVectorNorm_Unit3Axes(t *testing.T) {
	v := []float32{3, 4, 0}
	want := float32(5)
	if got := VectorNorm(v); !floatNear(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	if v[0] != 3 || v[1] != 4 || v[2] != 0 {
		t.Fatal("VectorNorm must not mutate input")
	}
}

func TestVectorNorm_Empty(t *testing.T) {
	if got := VectorNorm(nil); got != 0 {
		t.Fatalf("want 0, got %v", got)
	}
}

// --- NormalizeVector ---

func TestNormalizeVector_UnitLengthAfter(t *testing.T) {
	v := []float32{3, 4}
	NormalizeVector(v)
	if got := VectorNorm(v); !floatNear(got, 1) {
		t.Fatalf("post-normalize norm must be 1, got %v", got)
	}
	if !floatNear(v[0], 0.6) || !floatNear(v[1], 0.8) {
		t.Fatalf("unexpected values: %v", v)
	}
}

func TestNormalizeVector_ZeroIsNoop(t *testing.T) {
	v := []float32{0, 0, 0}
	NormalizeVector(v)
	for i, x := range v {
		if x != 0 {
			t.Fatalf("index %d: expected 0, got %v", i, x)
		}
	}
}

func TestNormalizeVector_AlreadyUnit(t *testing.T) {
	v := []float32{1, 0, 0}
	NormalizeVector(v)
	if !floatNear(v[0], 1) || !floatNear(v[1], 0) || !floatNear(v[2], 0) {
		t.Fatalf("identity failed: %v", v)
	}
}

// --- CosineSimilarity ---

func TestCosineSimilarity_Identical(t *testing.T) {
	v := []float32{1, 2, 3}
	if got := CosineSimilarity(v, v); !floatNear(got, 1) {
		t.Fatalf("identical should be 1, got %v", got)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	if got := CosineSimilarity(a, b); !floatNear(got, 0) {
		t.Fatalf("orthogonal should be 0, got %v", got)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{-1, 0}
	if got := CosineSimilarity(a, b); !floatNear(got, -1) {
		t.Fatalf("opposite should be -1, got %v", got)
	}
}

func TestCosineSimilarity_LengthMismatch(t *testing.T) {
	if got := CosineSimilarity([]float32{1, 2}, []float32{1, 2, 3}); got != 0 {
		t.Fatalf("mismatch length should return 0, got %v", got)
	}
}

func TestCosineSimilarity_ZeroVectorReturnsZero(t *testing.T) {
	if got := CosineSimilarity([]float32{0, 0}, []float32{1, 2}); got != 0 {
		t.Fatalf("zero vector should return 0, got %v", got)
	}
	if got := CosineSimilarity([]float32{1, 2}, []float32{0, 0}); got != 0 {
		t.Fatalf("zero vector should return 0, got %v", got)
	}
}

func TestCosineSimilarity_KnownAngle(t *testing.T) {
	// 45 degrees between (1,0) and (1,1): 1/sqrt(2)
	a := []float32{1, 0}
	b := []float32{1, 1}
	want := float32(1 / math.Sqrt2)
	if got := CosineSimilarity(a, b); !floatNear(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

// --- CosineSimilarityWithNorm ---

func TestCosineSimilarityWithNorm_PrecomputedNorm(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{1, 1}
	// norm of b = sqrt(2); cos = (1*1 + 0*1)/(1*sqrt(2)) = 1/sqrt(2)
	got := CosineSimilarityWithNorm(a, b, float32(math.Sqrt2))
	want := float32(1 / math.Sqrt2)
	if !floatNear(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestCosineSimilarityWithNorm_RejectsZeroNorm(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 0}
	if got := CosineSimilarityWithNorm(a, b, 0); got != 0 {
		t.Fatalf("zero target norm should return 0, got %v", got)
	}
}

func TestCosineSimilarityWithNorm_LengthMismatch(t *testing.T) {
	if got := CosineSimilarityWithNorm([]float32{1}, []float32{1, 2}, 1); got != 0 {
		t.Fatalf("mismatch len should return 0, got %v", got)
	}
}

// --- BatchDotProducts ---

func TestBatchDotProducts_SingleRow(t *testing.T) {
	q := []float32{1, 2, 3}
	m := []float32{4, 5, 6} // 1 row × 3 cols
	dots := []float32{0}
	BatchDotProducts(q, m, 1, 3, dots)
	if !floatNear(dots[0], 4+10+18) {
		t.Fatalf("want 32, got %v", dots[0])
	}
}

func TestBatchDotProducts_MultipleRows(t *testing.T) {
	q := []float32{1, 1}
	m := []float32{1, 0, 0, 1, 1, 1} // rows: (1,0) (0,1) (1,1)
	dots := make([]float32, 3)
	BatchDotProducts(q, m, 3, 2, dots)
	want := []float32{1, 1, 2}
	for i := range dots {
		if !floatNear(dots[i], want[i]) {
			t.Fatalf("row %d: want %v, got %v", i, want[i], dots[i])
		}
	}
}

func TestBatchDotProducts_ZeroRows(t *testing.T) {
	q := []float32{1, 2, 3}
	dots := []float32{}
	BatchDotProducts(q, nil, 0, 3, dots)
	if len(dots) != 0 {
		t.Fatal("zero rows: dots should remain empty")
	}
}

func TestBatchDotProducts_DoesNotMutateMatrix(t *testing.T) {
	q := []float32{1, 0}
	m := []float32{5, 7}
	dots := []float32{0}
	BatchDotProducts(q, m, 1, 2, dots)
	if m[0] != 5 || m[1] != 7 {
		t.Fatalf("matrix mutated: %v", m)
	}
	if dots[0] != 5 {
		t.Fatalf("dot not stored: %v", dots[0])
	}
}
