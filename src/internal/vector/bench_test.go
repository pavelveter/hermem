package vector

import (
	"context"
	"fmt"
	"testing"

	"github.com/pavelveter/hermem/pkg/spi"
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

// benchInMemorySearch seeds a namespace with n 768-dim vectors and returns the
// store plus a unit query vector.
func benchInMemorySearch(b *testing.B, n int) (spi.VectorStore, []float32) {
	b.Helper()
	vi := NewInMemoryVectorStore(nil, 0)
	recs := make([]spi.VectorRecord, n)
	for i := range recs {
		vec := make([]float32, 768)
		for j := range vec {
			vec[j] = float32((i+j)%17) / 17
		}
		recs[i] = spi.VectorRecord{Namespace: spi.DefaultNamespace, ID: fmt.Sprintf("id-%d", i), Vector: vec}
	}
	if err := vi.Upsert(context.Background(), recs); err != nil {
		b.Fatalf("seed: %v", err)
	}
	query := make([]float32, 768)
	for i := range query {
		query[i] = 0.5
	}
	return vi, query
}

// BenchmarkInMemorySearch guards the store's zero-copy search path: it must
// score the namespace matrix in place rather than copying every vector into a
// fresh matrix per query (which allocated the whole index on each call).
func BenchmarkInMemorySearch(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			vi, query := benchInMemorySearch(b, n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := vi.Search(context.Background(), spi.SearchRequest{
					Namespace: spi.DefaultNamespace, Vector: query, Limit: 10,
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
