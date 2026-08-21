package spiadapter

import (
	"context"
	"testing"

	"github.com/pavelveter/hermem/pkg/domain"
	"github.com/pavelveter/hermem/pkg/spi"
)

type testLegacyExtractor struct{}

func (testLegacyExtractor) ExtractEntities(_ context.Context, _ string) (*domain.ExtractionResult, error) {
	return &domain.ExtractionResult{Entities: []domain.ExtractedEntity{{
		ID: "model-id", Category: "world", Content: "fact",
		Relations: []domain.Relation{{TargetID: "legacy-ref", RelationType: "uses"}},
	}}}, nil
}

// Embedder bridges were deleted after zero-reference verification —
// providers satisfy spi.Embedder directly (see builtins_test.go).

func TestNewExtractor_DropsModelID(t *testing.T) {
	public := NewExtractor(testLegacyExtractor{})
	result, err := public.Extract(context.Background(), spi.ExtractRequest{Dialog: "dialog"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entities) != 1 || result.Entities[0].Content != "fact" {
		t.Fatalf("unexpected drafts: %+v", result.Entities)
	}
	if result.Entities[0].Relations[0].TargetRef != "legacy-ref" {
		t.Fatalf("legacy relation reference was not preserved: %+v", result.Entities[0].Relations)
	}
}

// Reranker bridging was removed in task 4.3 — all canonical reranker
// implementations now satisfy spi.Reranker directly. This test is
// superseded by pkg/spi/contract_test.go::compileReranker and the
// ai/aitest FakeReranker conformance assertions.

// type testLegacyReranker and bridging were removed in 4.3; kept as
// blank placeholder so the test layout remains stable.
