package main

import (
	"fmt"
	"math"
	"testing"
)

// BenchmarkQuantizeRoundtrip measures QuantizeVector +
// DequantizeVector wall-clock time across increasing vector
// dimensions. Synthetic input uses sinusoidal values in [-1, +1].
//
// Scale: 128, 256, 512, 768, 1536, 3072, 4096 dims.
//
// Also benchmarks serialization: QuantizedEmbeddingToBytes +
// BytesToQuantizedEmbedding to cover the encode/decode hot path.
func BenchmarkQuantizeRoundtrip(b *testing.B) {
	for _, dim := range []int{128, 256, 512, 768, 1536, 3072, 4096} {
		b.Run(fmt.Sprintf("D=%d", dim), func(b *testing.B) {
			// One synthetic vector per benchmark — avoid alloc in loop.
			vec := make([]float32, dim)
			for i := range vec {
				vec[i] = float32(math.Sin(float64(i)*0.01)) * 0.5
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				qv := QuantizeVector(vec)
				deq := DequantizeVector(qv)
				_ = deq
			}
		})
	}
}

// BenchmarkQuantizeSerialization measures encode/decode throughput
// independent of the quantize step. Useful for sizing the BLOB
// serialization cost that dominates storage-heavy workloads.
func BenchmarkQuantizeSerialization(b *testing.B) {
	for _, dim := range []int{128, 768, 1536, 3072} {
		b.Run(fmt.Sprintf("D=%d", dim), func(b *testing.B) {
			vec := make([]float32, dim)
			for i := range vec {
				vec[i] = float32(math.Sin(float64(i)*0.01)) * 0.5
			}
			qv := QuantizeVector(vec)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				blob := QuantizedEmbeddingToBytes(qv)
				q2 := BytesToQuantizedEmbedding(blob)
				_ = q2
			}
		})
	}
}

// BenchmarkQuantizeBatch measures QuantizeBatch + DequantizeBatch
// on batch sizes from 10 to 200 embeddings at 768 dims.
func BenchmarkQuantizeBatch(b *testing.B) {
	dim := 768
	for _, batchSize := range []int{10, 50, 100, 200} {
		b.Run(fmt.Sprintf("B=%d", batchSize), func(b *testing.B) {
			embeddings := make([][]float32, batchSize)
			for j := range embeddings {
				embeddings[j] = make([]float32, dim)
				for i := range embeddings[j] {
					embeddings[j][i] = float32(math.Sin(float64(i*j)*0.01)) * 0.5
				}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				qvs := QuantizeBatch(embeddings)
				deqs := DequantizeBatch(qvs)
				_ = deqs
			}
		})
	}
}
