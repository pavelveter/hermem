//go:build darwin

package main

/*
#cgo CFLAGS: -DACCELERATE_NEW_LAPACK
#cgo LDFLAGS: -framework Accelerate
#include <Accelerate/Accelerate.h>

static inline void batched_dot(const float *V, const float *q, int rows, int cols, float *dot) {
    cblas_sgemv(CblasRowMajor, CblasNoTrans, rows, cols,
                1.0f, V, cols, q, 1, 0.0f, dot, 1);
}
*/
import "C"
import (
	"math"
	"unsafe"
)

func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	n := C.int(len(a))
	pa := (*C.float)(unsafe.Pointer(&a[0]))
	pb := (*C.float)(unsafe.Pointer(&b[0]))

	dot := float32(C.cblas_sdot(n, pa, 1, pb, 1))
	normA2 := float32(C.cblas_sdot(n, pa, 1, pa, 1))
	normB2 := float32(C.cblas_sdot(n, pb, 1, pb, 1))

	if normA2 == 0 || normB2 == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(normA2))) * float32(math.Sqrt(float64(normB2))))
}

// CosineSimilarityWithNorm mirrors CosineSimilarity but uses a
// precomputed `normA` (L2 norm of vector a) instead of recomputing
// it via cblas_sdot(a,a). Saves one cblas_sdot call per row when
// the same query participates in many similarity computations;
// behaviour parity with CosineSimilarity holds exactly when
// `normA == VectorNorm(a)`.
func CosineSimilarityWithNorm(a, b []float32, normA float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	n := C.int(len(a))
	pa := (*C.float)(unsafe.Pointer(&a[0]))
	pb := (*C.float)(unsafe.Pointer(&b[0]))

	dot := float32(C.cblas_sdot(n, pa, 1, pb, 1))
	normB2 := float32(C.cblas_sdot(n, pb, 1, pb, 1))

	if normA == 0 || normB2 == 0 {
		return 0
	}
	return dot / (normA * float32(math.Sqrt(float64(normB2))))
}

func VectorNorm(v []float32) float32 {
	if len(v) == 0 {
		return 0
	}
	n := C.int(len(v))
	pv := (*C.float)(unsafe.Pointer(&v[0]))
	norm2 := float32(C.cblas_sdot(n, pv, 1, pv, 1))
	if norm2 == 0 {
		return 0
	}
	return float32(math.Sqrt(float64(norm2)))
}

func NormalizeVector(v []float32) {
	n := VectorNorm(v)
	if n == 0 {
		return
	}
	for i := range v {
		v[i] /= n
	}
}

func BatchDotProducts(query []float32, matrix []float32, rows, cols int, dot []float32) {
	_ = matrix[rows*cols-1]
	_ = dot[rows-1]
	C.batched_dot(
		(*C.float)(unsafe.Pointer(&matrix[0])),
		(*C.float)(unsafe.Pointer(&query[0])),
		C.int(rows), C.int(cols),
		(*C.float)(unsafe.Pointer(&dot[0])),
	)
}
