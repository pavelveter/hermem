package store

import (
	"encoding/binary"
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
	emb := make([]float32, expectedDim)
	for i := range emb {
		emb[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : i*4+4]))
	}
	return emb, nil
}
