package store

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// EmbeddingToBytes converts a float32 slice to little-endian bytes (4 bytes per element).
func EmbeddingToBytes(embedding []float32) []byte {
	if len(embedding) == 0 {
		return nil
	}
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// BytesToEmbedding converts bytes back to a float32 slice without dimension validation.
// Use DecodeVector when dimension must be checked.
func BytesToEmbedding(data []byte) []float32 {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil
	}
	embedding := make([]float32, len(data)/4)
	for i := range embedding {
		embedding[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : i*4+4]))
	}
	return embedding
}

// DecodeVector decodes a vector BLOB into a float32 slice with dimension validation.
// Returns an error on dimension drift instead of silently truncating.
func DecodeVector(data []byte, expectedDim int) ([]float32, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty vector blob")
	}
	if len(data) != expectedDim*4 {
		return nil, fmt.Errorf("vector dimension drift: blob %d bytes, want %d", len(data), expectedDim*4)
	}
	return BytesToFloat32Safe(data)
}

// ErrFloatNaNOrInf is the sentinel wrapped by BytesToFloat32Safe's
// rejection path. Callers that want to branch on the kind of failure
// (e.g. "skip this row but accept the next" vs "abort whole load")
// should errors.Is(err, ErrFloatNaNOrInf) rather than substring-match
// the error string.
var ErrFloatNaNOrInf = errors.New("vector byte offset is NaN/Inf")

// BytesToFloat32Safe decodes a vector byte blob to float32 while
// rejecting NaN/Inf payloads mid-stream. A corrupted blob whose
// Float32 bits encode NaN/Inf would otherwise survive into flatMatrix
// and taint downstream BatchDotProducts (NaN * 0 = NaN → poisoned scores).
//
// Returns errors.Is(err, ErrFloatNaNOrInf) wrapped with the byte
// offset of the offending element so the caller can decide whether to
// skip, regenerate, or refuse the whole entity.
func BytesToFloat32Safe(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("vector blob length %d not multiple of 4", len(data))
	}
	out := make([]float32, len(data)/4)
	for i := range out {
		bits := binary.LittleEndian.Uint32(data[i*4 : i*4+4])
		f := math.Float32frombits(bits)
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			return nil, fmt.Errorf("%w offset=%d", ErrFloatNaNOrInf, i*4)
		}
		out[i] = f
	}
	return out, nil
}
