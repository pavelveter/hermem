package vector

import (
	"testing"
)

func BenchmarkCosineSimilarity_128(b *testing.B) {
	a := make([]float32, 128)
	b2 := make([]float32, 128)
	for i := range a {
		a[i] = float32(i) / 128.0
		b2[i] = float32(i) / 256.0
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CosineSimilarity(a, b2)
	}
}

func BenchmarkCosineSimilarity_768(b *testing.B) {
	a := make([]float32, 768)
	b2 := make([]float32, 768)
	for i := range a {
		a[i] = float32(i) / 768.0
		b2[i] = float32(i) / 1536.0
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CosineSimilarity(a, b2)
	}
}

func BenchmarkNormalizeVector(b *testing.B) {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NormalizeVector(v)
	}
}

func BenchmarkQuantizeVector(b *testing.B) {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(i) / 768.0
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		QuantizeVector(v)
	}
}

func BenchmarkDequantizeVector(b *testing.B) {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(i) / 768.0
	}
	qv := QuantizeVector(v)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DequantizeVector(qv)
	}
}
