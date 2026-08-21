// Package externalprovider is an external-like fixture used to ensure
// providers can implement the public contracts without internal imports.
package externalprovider

import (
	"context"

	"github.com/pavelveter/hermem/pkg/domain"
	"github.com/pavelveter/hermem/pkg/spi"
)

type Provider struct{}

func (Provider) Embed(context.Context, string) ([]float32, error) { return []float32{1}, nil }
func (Provider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1}
	}
	return out, nil
}
func (Provider) Extract(context.Context, spi.ExtractRequest) (spi.ExtractResponse, error) {
	return spi.ExtractResponse{Entities: []domain.EntityDraft{{Category: "external"}}}, nil
}
func (Provider) Rerank(_ context.Context, _ string, candidates []spi.Candidate) ([]spi.Candidate, error) {
	return candidates, nil
}
func (Provider) Search(context.Context, spi.SearchRequest) ([]spi.Hit, error) {
	return []spi.Hit{{ID: "external", Score: 1}}, nil
}
func (Provider) Upsert(context.Context, []spi.VectorRecord) error { return nil }
func (Provider) Delete(context.Context, spi.DeleteRequest) error  { return nil }
func (Provider) Stats(context.Context, string) (spi.VectorStats, error) {
	return spi.VectorStats{Dimensions: 1}, nil
}

var (
	_ spi.Embedder      = Provider{}
	_ spi.BatchEmbedder = Provider{}
	_ spi.Extractor     = Provider{}
	_ spi.Reranker      = Provider{}
	_ spi.VectorStore   = Provider{}
)
