package spiadapter

import (
	"context"
	"reflect"
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

type testVectorStore struct {
	lastSearch spi.SearchRequest
	upserts    []spi.VectorRecord
	deletes    []spi.DeleteRequest
}

func (s *testVectorStore) Search(_ context.Context, req spi.SearchRequest) ([]spi.Hit, error) {
	s.lastSearch = req
	return []spi.Hit{{ID: "b", Score: 0.9}, {ID: "a", Score: 0.8}}, nil
}
func (s *testVectorStore) Upsert(_ context.Context, records []spi.VectorRecord) error {
	s.upserts = append(s.upserts, records...)
	return nil
}
func (s *testVectorStore) Delete(_ context.Context, req spi.DeleteRequest) error {
	s.deletes = append(s.deletes, req)
	return nil
}
func (s *testVectorStore) Stats(context.Context, string) (spi.VectorStats, error) {
	return spi.VectorStats{}, nil
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

func TestNewLegacyVectorIndex_PreservesIDsOnlySemantics(t *testing.T) {
	store := &testVectorStore{}
	legacy := NewLegacyVectorIndex(store)
	ids, err := legacy.Search(context.Background(), []float32{1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{"b", "a"}) {
		t.Fatalf("legacy IDs = %v", ids)
	}
	if store.lastSearch.Namespace != legacyNamespace || store.lastSearch.Limit != 2 {
		t.Fatalf("legacy search request = %+v", store.lastSearch)
	}
	if err := legacy.Store(context.Background(), "c", []float32{1}); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Remove(context.Background(), []string{"c"}); err != nil {
		t.Fatal(err)
	}
	if len(store.upserts) != 1 || len(store.deletes) != 1 {
		t.Fatalf("store operations: upserts=%d deletes=%d", len(store.upserts), len(store.deletes))
	}
}
