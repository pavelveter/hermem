package main

import (
	"math"
	"testing"
)

func TestQuantizeDequantizeRoundtrip(t *testing.T) {
	vec := []float32{0.1, -0.5, 0.8, 0.0, -0.3, 1.0, -0.9, 0.4}

	qv := QuantizeVector(vec)
	deq := DequantizeVector(qv)

	if len(deq) != len(vec) {
		t.Fatalf("dequantized length %d != original %d", len(deq), len(vec))
	}

	// Scalar int8 quantisation: max error ≤ range/255 ≈ 1.9/255 ≈ 0.0075.
	// Use generous tolerance of 0.02 to account for rounding.
	const epsilon = 0.02
	for i := range vec {
		diff := float64(deq[i] - vec[i])
		if diff < 0 {
			diff = -diff
		}
		if diff > epsilon {
			t.Errorf("idx %d: deq=%.6f, orig=%.6f, diff=%.6f > %.6f",
				i, deq[i], vec[i], diff, epsilon)
		}
	}
}

func TestQuantizeZeroVector(t *testing.T) {
	qv := QuantizeVector([]float32{0, 0, 0, 0})
	deq := DequantizeVector(qv)

	for i, v := range deq {
		if v != 0 {
			t.Errorf("idx %d: expected 0, got %.6f", i, v)
		}
	}
	if qv.Min != 0 || qv.Max != 0 {
		t.Errorf("min=%.4f max=%.4f, want both 0", qv.Min, qv.Max)
	}
}

func TestQuantizeEmpty(t *testing.T) {
	qv := QuantizeVector(nil)
	if len(qv.Codes) != 0 {
		t.Fatal("expected empty codes for nil input")
	}
	deq := DequantizeVector(qv)
	if deq != nil {
		t.Fatal("expected nil for empty dequantize")
	}
}

func TestQuantizeConstantVector(t *testing.T) {
	vec := []float32{0.5, 0.5, 0.5, 0.5, 0.5}
	qv := QuantizeVector(vec)
	deq := DequantizeVector(qv)

	if qv.Min != 0.5 || qv.Max != 0.5 {
		t.Errorf("min=%.4f max=%.4f, want both 0.5", qv.Min, qv.Max)
	}
	const epsilon = float32(1e-6)
	for i, v := range deq {
		diff := v - vec[i]
		if diff < 0 {
			diff = -diff
		}
		if diff > epsilon {
			t.Errorf("idx %d: deq=%.6f, orig=%.6f, diff=%.6f", i, v, vec[i], diff)
		}
	}
}

func TestQuantizeSerializationRoundtrip(t *testing.T) {
	vec := []float32{-1.0, -0.5, 0.0, 0.5, 1.0}
	qv := QuantizeVector(vec)
	blob := QuantizedEmbeddingToBytes(qv)

	// Blob size: 8 bytes header + len(vec) codes.
	expectedLen := 8 + len(vec)
	if len(blob) != expectedLen {
		t.Fatalf("blob len %d != expected %d", len(blob), expectedLen)
	}

	qv2 := BytesToQuantizedEmbedding(blob)
	deq := DequantizeVector(qv2)

	if len(deq) != len(vec) {
		t.Fatalf("dequantized length %d != original %d", len(deq), len(vec))
	}

	const epsilon = 0.02
	for i := range vec {
		diff := float64(deq[i] - vec[i])
		if diff < 0 {
			diff = -diff
		}
		if diff > epsilon {
			t.Errorf("idx %d: deq=%.6f, orig=%.6f, diff=%.6f", i, deq[i], vec[i], diff)
		}
	}
}

func TestBytesToQuantizedEmbeddingTooShort(t *testing.T) {
	qv := BytesToQuantizedEmbedding([]byte{0, 0, 0, 0})
	if len(qv.Codes) != 0 {
		t.Fatal("expected empty for short blob")
	}
}

func TestQuantizeCompressionRatio(t *testing.T) {
	dim := 768
	raw := make([]float32, dim)
	for i := range raw {
		raw[i] = float32(math.Sin(float64(i)*0.01)) * 0.5
	}
	qv := QuantizeVector(raw)
	blob := QuantizedEmbeddingToBytes(qv)

	rawBytes := dim * 4
	qBytes := len(blob)
	ratio := float64(rawBytes) / float64(qBytes)

	if ratio < 3.5 || ratio > 4.1 {
		t.Errorf("compression ratio %.2f (want ~4.0) for dim=%d", ratio, dim)
	}
}

func TestQuantizeBatch(t *testing.T) {
	embeddings := [][]float32{
		{0.1, 0.2, 0.3},
		{-0.5, 0.0, 0.5},
		{1.0, -1.0, 0.0},
	}
	qvs := QuantizeBatch(embeddings)
	deqs := DequantizeBatch(qvs)

	if len(deqs) != 3 {
		t.Fatalf("len %d != 3", len(deqs))
	}
	for j := range embeddings {
		for i := range embeddings[j] {
			diff := float64(deqs[j][i] - embeddings[j][i])
			if diff < 0 {
				diff = -diff
			}
			if diff > 0.02 {
				t.Errorf("batch %d idx %d: deq=%.6f orig=%.6f diff=%.6f",
					j, i, deqs[j][i], embeddings[j][i], diff)
			}
		}
	}
}
