package spi_test

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/pkg/domain"
	"github.com/pavelveter/hermem/pkg/spi"
)

type compileEmbedder struct{}

func (compileEmbedder) Embed(context.Context, string) ([]float32, error) { return nil, nil }
func (compileEmbedder) EmbedBatch(context.Context, []string) ([][]float32, error) {
	return nil, nil
}

type compileExtractor struct{}

func (compileExtractor) Extract(context.Context, spi.ExtractRequest) (spi.ExtractResponse, error) {
	return spi.ExtractResponse{Entities: []domain.EntityDraft{{Category: "test", Content: "draft"}}}, nil
}

type compileReranker struct{}

func (compileReranker) Rerank(context.Context, string, []spi.Candidate) ([]spi.Candidate, error) {
	return nil, nil
}

type compileVectorStore struct{}

func (compileVectorStore) Search(context.Context, spi.SearchRequest) ([]spi.Hit, error) {
	return []spi.Hit{{ID: "entity", Score: 1}}, nil
}
func (compileVectorStore) Upsert(context.Context, []spi.VectorRecord) error { return nil }
func (compileVectorStore) Delete(context.Context, spi.DeleteRequest) error  { return nil }
func (compileVectorStore) Stats(context.Context, string) (spi.VectorStats, error) {
	return spi.VectorStats{}, nil
}

var (
	_ spi.Embedder      = compileEmbedder{}
	_ spi.BatchEmbedder = compileEmbedder{}
	_ spi.Extractor     = compileExtractor{}
	_ spi.Reranker      = compileReranker{}
	_ spi.VectorStore   = compileVectorStore{}
)

func TestPublicContractsCompile(t *testing.T) {
	var entityID domain.EntityID = "entity-1"
	var taskID domain.TaskID = "task-1"
	if entityID == "" || taskID == "" {
		t.Fatal("public identifiers must be constructible by external consumers")
	}

	request := spi.SearchRequest{
		Namespace: "default/model",
		Vector:    []float32{1, 0},
		Limit:     1,
		Filter: spi.Filter{Equals: map[string]string{
			"category": "observation",
		}},
	}
	if request.Namespace == "" || request.Limit != 1 {
		t.Fatal("public vector request contract is not usable")
	}
}
