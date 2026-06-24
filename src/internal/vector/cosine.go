//go:build !darwin || !cgo

package vector

import "math"

// CosineSimilarity computes cosine similarity between two equal-length vectors.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
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

// CosineSimilarityWithNorm computes cosine similarity using a precomputed norm for b (e.g. a unit-vector entry).
func CosineSimilarityWithNorm(a, b []float32, normB float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(normA))*float64(normB))
}

// VectorNorm returns L2 norm of v.
func VectorNorm(v []float32) float32 {
	var sum float32
	for _, x := range v {
		sum += x * x
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

// BatchDotProducts computes dot(query, matrix[r]) for every row r of an rows×cols matrix.
// Matrix is stored row-major. Output slice length must equal rows.
//
// Mirrors the bounds-bump contract from cosine_darwin.go: if any of the
// inputs is undersized the function panics immediately rather than
// walking past the supplied slices inside the inner loop. Callers (public
// Search in inmemory.go) MUST validate dimensions first via the
// sentinel ErrInvalidQueryDim / ErrMatrixCorrupted and only reach this
// function with consistent shapes.
func BatchDotProducts(query []float32, matrix []float32, rows, cols int, dot []float32) {
	if rows == 0 || cols == 0 {
		return
	}
	_ = query[cols-1]
	_ = matrix[rows*cols-1]
	_ = dot[rows-1]
	for r := 0; r < rows; r++ {
		var d float32
		for c := 0; c < cols; c++ {
			d += query[c] * matrix[r*cols+c]
		}
		dot[r] = d
	}
}
