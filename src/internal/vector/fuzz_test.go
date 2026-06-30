package vector

import "testing"

func FuzzCosineSimilarity(f *testing.F) {
	f.Add(float32(1), float32(0), float32(0), float32(0), float32(1), float32(0))
	f.Add(float32(1), float32(2), float32(3), float32(4), float32(5), float32(6))

	f.Fuzz(func(t *testing.T, a0, a1, a2, b0, b1, b2 float32) {
		a := []float32{a0, a1, a2}
		b := []float32{b0, b1, b2}
		sim := CosineSimilarity(a, b)
		if sim < -1-eps || sim > 1+eps {
			t.Errorf("CosineSimilarity returned %v outside [-1,1]", sim)
		}
		simBA := CosineSimilarity(b, a)
		if !floatNear(sim, simBA) {
			t.Errorf("asymmetry: cos(a,b)=%v, cos(b,a)=%v", sim, simBA)
		}
	})
}

func FuzzNormalizeVector(f *testing.F) {
	f.Add(float32(3), float32(4), float32(0))
	f.Add(float32(1), float32(1), float32(1))
	f.Add(float32(0), float32(0), float32(0))

	f.Fuzz(func(t *testing.T, v0, v1, v2 float32) {
		v := []float32{v0, v1, v2}
		NormalizeVector(v)
		norm := VectorNorm(v)
		if norm > 0 && (norm < 1-eps || norm > 1+eps) {
			t.Errorf("after NormalizeVector, norm = %v, want 0 or 1", norm)
		}
	})
}
