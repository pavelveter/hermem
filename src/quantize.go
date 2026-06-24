package main

import (
	"math"
)

// QuantizedVector packs a float32 embedding into int8 components
// plus global min/max for reconstruction. Scalar (per-vector)
// quantisation with 8-bit linear binning gives ~4× compression
// (4 bytes → 1 byte per component) at the cost of small (~1%)
// recall degradation — acceptable for P2.
//
// Format:
//
//	[min: float32] [max: float32] [q0: int8] [q1: int8] ... [q_{d-1}: int8]
//	= 8 + d bytes total vs 4*d bytes for raw float32.
type QuantizedVector struct {
	Min   float32
	Max   float32
	Codes []int8
}

// QuantizeVector performs scalar per-vector quantisation of a float32
// embedding into the QuantizedVector format. A zero vector is returned
// as-is (min=max=0, codes=all 0).
func QuantizeVector(embedding []float32) QuantizedVector {
	if len(embedding) == 0 {
		return QuantizedVector{Codes: []int8{}}
	}

	// Find global min and max.
	vmin := float32(math.MaxFloat32)
	vmax := float32(-math.MaxFloat32)
	for _, v := range embedding {
		if v < vmin {
			vmin = v
		}
		if v > vmax {
			vmax = v
		}
	}

	codes := make([]int8, len(embedding))

	// Zero-range vector: all values identical.
	if vmax == vmin {
		return QuantizedVector{Min: vmin, Max: vmax, Codes: codes}
	}

	// Linear quantisation: q = round((x - min) / (max - min) * 255) - 128
	scale := 255.0 / float64(vmax-vmin)
	for i, v := range embedding {
		// Map to [0, 255], shift to [-128, 127].
		norm := float64(v-vmin) * scale
		q := int(math.Round(norm)) - 128
		if q < -128 {
			q = -128
		}
		if q > 127 {
			q = 127
		}
		codes[i] = int8(q)
	}

	return QuantizedVector{Min: vmin, Max: vmax, Codes: codes}
}

// DequantizeVector reconstructs a float32 embedding from its
// quantised representation.
func DequantizeVector(qv QuantizedVector) []float32 {
	if len(qv.Codes) == 0 {
		return nil
	}

	embedding := make([]float32, len(qv.Codes))

	if qv.Max == qv.Min {
		// Constant vector.
		for i := range embedding {
			embedding[i] = qv.Min
		}
		return embedding
	}

	// Reverse: x = min + (q + 128) / 255 * (max - min)
	scale := float64(qv.Max-qv.Min) / 255.0
	for i, q := range qv.Codes {
		embedding[i] = qv.Min + float32(float64(int(q)+128)*scale)
	}
	return embedding
}

// QuantizedEmbeddingToBytes serialises a QuantizedVector into a BLOB
// suitable for the entities.embedding column.
//
// Layout: min(4) + max(4) + codes(d) = 8 + d bytes.
func QuantizedEmbeddingToBytes(qv QuantizedVector) []byte {
	buf := make([]byte, 8+len(qv.Codes))

	// Store min and max as float32 little-endian.
	bits := math.Float32bits(qv.Min)
	buf[0] = byte(bits)
	buf[1] = byte(bits >> 8)
	buf[2] = byte(bits >> 16)
	buf[3] = byte(bits >> 24)

	bits = math.Float32bits(qv.Max)
	buf[4] = byte(bits)
	buf[5] = byte(bits >> 8)
	buf[6] = byte(bits >> 16)
	buf[7] = byte(bits >> 24)

	// Store codes as raw int8 bytes.
	for i, q := range qv.Codes {
		buf[8+i] = byte(q)
	}
	return buf
}

// BytesToQuantizedEmbedding deserialises a BLOB back into a
// QuantizedVector. Returns zero value if the blob is too short
// (fewer than 9 bytes: 8 header + ≥1 code).
func BytesToQuantizedEmbedding(data []byte) QuantizedVector {
	if len(data) < 9 {
		return QuantizedVector{}
	}

	minBits := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
	maxBits := uint32(data[4]) | uint32(data[5])<<8 | uint32(data[6])<<16 | uint32(data[7])<<24

	codes := make([]int8, len(data)-8)
	for i := range codes {
		codes[i] = int8(data[8+i])
	}
	return QuantizedVector{
		Min:   math.Float32frombits(minBits),
		Max:   math.Float32frombits(maxBits),
		Codes: codes,
	}
}

// QuantizeBatch applies QuantizeVector to a slice of embeddings.
func QuantizeBatch(embeddings [][]float32) []QuantizedVector {
	out := make([]QuantizedVector, len(embeddings))
	for i, emb := range embeddings {
		out[i] = QuantizeVector(emb)
	}
	return out
}

// DequantizeBatch applies DequantizeVector to a slice of
// quantised embeddings.
func DequantizeBatch(qvs []QuantizedVector) [][]float32 {
	out := make([][]float32, len(qvs))
	for i, qv := range qvs {
		out[i] = DequantizeVector(qv)
	}
	return out
}
