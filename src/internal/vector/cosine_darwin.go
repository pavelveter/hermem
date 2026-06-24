//go:build darwin && cgo

// Package vector — Apple Accelerate (cblas) AMX/NEON fast path.
//
// Why this file exists: cblas_sgemv / cblas_sdot / cblas_snrm2 / cblas_sscal
// route through Apple's hand-tuned BLAS, which on M-series silicon uses
// the AMX coprocessor for large matmul-like workloads. Kernel-level (batch
// cosine across thousands of entities) this is ~5-15× faster than the
// pure-Go loop in cosine.go; end-to-end retrieval is smaller because SQL
// and allocations dominate.
//
// Build selection: this file is selected ONLY on darwin WITH cgo enabled.
// The pure-Go fallback in cosine.go has the matching strict inverse build
// tag (//go:build !darwin || !cgo) so only one copy of each function
// symbol exists in any build configuration.
//
// Compile with `CGO_ENABLED=1`. The Dockerfile and install.sh already
// enforce this for `mattn/go-sqlite3`; no new build infra is required.

package vector

/*
#cgo CFLAGS: -O3 -DACCELERATE_NEW_LAPACK
#cgo LDFLAGS: -framework Accelerate
#include <Accelerate/Accelerate.h>

// batched_dot computes dot(q, V[r]) for every row r of an rows×cols row-major matrix V.
// Uses cblas_sgemv in row-major mode so the caller does not have to transpose.
static inline void batched_dot(const float *V, const float *q,
                               int rows, int cols, float *dot) {
    cblas_sgemv(CblasRowMajor, CblasNoTrans, rows, cols,
                1.0f, V, cols, q, 1, 0.0f, dot, 1);
}
*/
import "C"

import (
	"unsafe"
)

// VectorNorm returns the L2 norm of v. Zero on empty input (cblas_snrm2(0,..) is undefined).
func VectorNorm(v []float32) float32 {
	if len(v) == 0 {
		return 0
	}
	n := C.int(len(v))
	pv := (*C.float)(unsafe.Pointer(&v[0]))
	return float32(C.cblas_snrm2(n, pv, 1))
}

// NormalizeVector scales v to unit length in place. No-op for empty / zero vectors.
// Uses cblas_sscal for the in-place scalar multiplication (BLAS standard primitive).
func NormalizeVector(v []float32) {
	if len(v) == 0 {
		return
	}
	n := C.int(len(v))
	pv := (*C.float)(unsafe.Pointer(&v[0]))
	// cblas_snrm2 for the norm; reuse the same pointer for the scale-out.
	nrm := float32(C.cblas_snrm2(n, pv, 1))
	if nrm == 0 {
		return
	}
	C.cblas_sscal(n, C.float(1.0/nrm), pv, 1)
}

// CosineSimilarity computes cosine similarity between a and b.
// Returns 0 for length mismatch, empty input, or either-side zero vector.
// Uses cblas_sdot (FMA-accelerated dot product) and cblas_snrm2 (L2 norm).
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	n := C.int(len(a))
	pa := (*C.float)(unsafe.Pointer(&a[0]))
	pb := (*C.float)(unsafe.Pointer(&b[0]))
	dot := float32(C.cblas_sdot(n, pa, 1, pb, 1))
	normA := float32(C.cblas_snrm2(n, pa, 1))
	normB := float32(C.cblas_snrm2(n, pb, 1))
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (normA * normB)
}

// CosineSimilarityWithNorm uses a precomputed norm for b (e.g. when b is a unit-vector
// index entry stored at ingest time). Cheaper than CosineSimilarity because the b norm
// skip saves a cblas_snrm2 call per pair.
func CosineSimilarityWithNorm(a, b []float32, normB float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	if normB == 0 {
		return 0
	}
	n := C.int(len(a))
	pa := (*C.float)(unsafe.Pointer(&a[0]))
	pb := (*C.float)(unsafe.Pointer(&b[0]))
	dot := float32(C.cblas_sdot(n, pa, 1, pb, 1))
	normA := float32(C.cblas_snrm2(n, pa, 1))
	if normA == 0 {
		return 0
	}
	return dot / (normA * normB)
}

// BatchDotProducts computes dot(query, matrix[r]) for every row r of an rows×cols matrix.
// Matrix is stored row-major. Output slice length must equal rows; cgo bridge assumes so.
//
// Bad input panics loudly: rows==0 || cols==0 is a clean no-op; undersized query,
// matrix, or dot slices panic via the bounds-bumps below — matching the pure-Go
// fallback's panic-on-bad-input contract in cosine.go.
func BatchDotProducts(query []float32, matrix []float32, rows, cols int, dot []float32) {
	if rows == 0 || cols == 0 {
		return
	}
	// Bounds-bumps surface len-mismatch as immediate Go panics instead of C-side
	// SIGSEGV from cblas_sgemv walking past the supplied slices. Matches the
	// pure-Go fallback's panic-on-bad-input behavior in cosine.go (which panics
	// inside the inner loop when any of these dimensions are undersized).
	_ = query[cols-1]
	_ = matrix[rows*cols-1]
	_ = dot[rows-1]
	C.batched_dot(
		(*C.float)(unsafe.Pointer(&matrix[0])),
		(*C.float)(unsafe.Pointer(&query[0])),
		C.int(rows), C.int(cols),
		(*C.float)(unsafe.Pointer(&dot[0])),
	)
}
