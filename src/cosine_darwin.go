//go:build darwin

package main

/*
#cgo CFLAGS: -DACCELERATE_NEW_LAPACK
#cgo LDFLAGS: -framework Accelerate
#include <Accelerate/Accelerate.h>
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
