//go:build darwin

package main

/*
#cgo CFLAGS: -DACCELERATE_NEW_LAPACK
#cgo LDFLAGS: -framework Accelerate
#include <Accelerate/Accelerate.h>

// batched_dot computes dot(V[row], q) for every row of the rows×cols
// matrix V, writing the results into dot[0..rows). This is a single
// cblas_sgemv call (matrix × vector) that replaces N per-vector
// cblas_sdot calls, dramatically reducing CGO overhead.
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

// VectorNorm computes the L2 norm of a single vector via cblas_sdot(v, v).
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

// BatchDotProducts computes dot(query, matrix[row]) for every row of the
// rows×cols matrix in a single cblas_sgemv call. The matrix must be
// stored row-major: matrix[row*cols + col]. The length of the output
// slice must equal rows.
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
