// Package aitest provides test doubles for AI clients.
package aitest

import (
	"context"

	"github.com/pavelveter/hermem/src/internal/core"
)

// FakeEmbedder returns a fixed embedding for any input.
type FakeEmbedder struct {
	Embedding []float32
	Calls     int
}

func (f *FakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	f.Calls++
	out := make([]float32, len(f.Embedding))
	copy(out, f.Embedding)
	return out, nil
}

func (f *FakeEmbedder) Ping(_ context.Context) error { return nil }

// FakeExtractor returns fixed entities for any input.
type FakeExtractor struct {
	Entities []core.ExtractedEntity
	Calls    int
}

func (f *FakeExtractor) ExtractEntities(_ context.Context, _ string) (*core.ExtractionResult, error) {
	f.Calls++
	return &core.ExtractionResult{Entities: f.Entities}, nil
}

// FakeReranker returns facts unchanged.
type FakeReranker struct {
	Calls int
}

func (f *FakeReranker) Rerank(_ context.Context, _ string, facts []core.RetrievedFact) ([]core.RetrievedFact, error) {
	f.Calls++
	return facts, nil
}
