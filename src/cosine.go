//go:build !darwin

package main

import "math"

func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// CosineSimilarityWithNorm computes the cosine of vectors a and b,
// using `normA` as the precomputed L2 norm of vector a. The norm of
// b is recomputed inside the function since the caller typically
// has no reason to share it across rows.
//
// Use when the same `a` (typically the query) participates in many
// similarity computations and its norm can be cached once per
// retrieval call. Falls back to 0 when lengths mismatch,
// len(a)==0, or either norm is 0.
//
// Behaviour parity invariant (used in TestCosineSimilarityWithNormParity):
// when `normA == VectorNorm(a)`, this returns the same score as
// CosineSimilarity(a, b). The difference is exactly one fewer
// sqrt per call when normA is supplied.
func CosineSimilarityWithNorm(a, b []float32, normA float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normB2 float32
	for i := range a {
		dot += a[i] * b[i]
		normB2 += b[i] * b[i]
	}
	if normA == 0 || normB2 == 0 {
		return 0
	}
	return dot / (normA * float32(math.Sqrt(float64(normB2))))
}

// VectorNorm computes the L2 norm of a single vector.
func VectorNorm(v []float32) float32 {
	var sum float32
	for _, x := range v {
		sum += x * x
	}
	if sum == 0 {
		return 0
	}
	return float32(math.Sqrt(float64(sum)))
}

// NormalizeVector scales v to unit length in place. No-op for zero vectors.
func NormalizeVector(v []float32) {
	n := VectorNorm(v)
	if n == 0 {
		return
	}
	for i := range v {
		v[i] /= n
	}
}

// BatchDotProducts computes dot(query, matrix[row]) for every row of the
// rows×cols matrix. The matrix must be stored row-major. The length of
// the output slice must equal rows.
func BatchDotProducts(query []float32, matrix []float32, rows, cols int, dot []float32) {
	_ = matrix[rows*cols-1]
	_ = dot[rows-1]
	for r := 0; r < rows; r++ {
		row := matrix[r*cols : (r+1)*cols]
		var d float32
		for c := 0; c < cols; c++ {
			d += query[c] * row[c]
		}
		dot[r] = d
	}
}
