// Package spiadapter bridges the legacy internal provider contracts to the
// public domain and SPI contracts during the compatibility release.
package spiadapter

import (
	"context"
	"fmt"

	"github.com/pavelveter/hermem/pkg/domain"
	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/core"
)

const legacyNamespace = spi.DefaultNamespace

// NewExtractor adapts the current LLM extractor result into identity-free
// public domain drafts. Prompt version and schema are retained for future
// providers but cannot be forwarded to the legacy extractor yet.
func NewExtractor(legacy core.LLMExtractor) spi.Extractor {
	return &extractorAdapter{legacy: legacy}
}

type extractorAdapter struct {
	legacy core.LLMExtractor
}

func (a *extractorAdapter) Extract(ctx context.Context, req spi.ExtractRequest) (spi.ExtractResponse, error) {
	if a == nil || a.legacy == nil {
		return spi.ExtractResponse{}, fmt.Errorf("spiadapter: nil legacy extractor")
	}
	result, err := a.legacy.ExtractEntities(ctx, req.Dialog)
	if err != nil {
		return spi.ExtractResponse{}, err
	}
	return spi.ExtractResponse{Entities: domain.LegacyExtractionDrafts(result)}, nil
}

// Reranker (spi → retrieval) bridge lives in src/internal/retrieval/legacy.go
// to avoid an spiadapter ↔ retrieval cycle. The retrieval package owns the
// value types and the bridge that adapts them.
//
// The embedder bridges (NewEmbedder / NewLegacyEmbedder) were deleted after
// zero-reference verification: every provider implements spi.Embedder
// directly and health checks use the optional spi.Pinger assertion, so
// neither direction needs an adapter anymore.

// NewLegacyExtractor adapts a public spi.Extractor to the legacy
// core.LLMExtractor shape that the ingestion, compression, and contradiction
// pipelines still consume. The conversion is LOSSY: the public SPI emits
// identity-free domain.EntityDraft values and the legacy shape carries
// LLM-suggested entity IDs, so the bridge fabricates synthetic IDs drawn
// from the candidate's position in the draft list. Only use this bridge
// for callers that no longer need the original LLM IDs (typically: tests
// and post-ADR-035 pipelines).
func NewLegacyExtractor(public spi.Extractor) core.LLMExtractor {
	return &legacyExtractorAdapter{public: public}
}

type legacyExtractorAdapter struct{ public spi.Extractor }

func (a *legacyExtractorAdapter) ExtractEntities(ctx context.Context, dialog string) (*core.ExtractionResult, error) {
	if a == nil || a.public == nil {
		return nil, fmt.Errorf("spiadapter: nil public extractor")
	}
	response, err := a.public.Extract(ctx, spi.ExtractRequest{Dialog: dialog})
	if err != nil {
		return nil, err
	}
	result := &core.ExtractionResult{Entities: make([]core.ExtractedEntity, 0, len(response.Entities))}
	for index, entity := range response.Entities {
		relations := make([]core.Relation, 0, len(entity.Relations))
		for _, relation := range entity.Relations {
			relations = append(relations, core.Relation{
				TargetID:     relation.TargetRef,
				RelationType: relation.RelationType,
			})
		}
		result.Entities = append(result.Entities, core.ExtractedEntity{
			ID:        fmt.Sprintf("spi-%d", index),
			Category:  entity.Category,
			Content:   entity.Content,
			Relations: relations,
		})
	}
	return result, nil
}

// NewLegacyVectorIndex adapts a public VectorStore to the old IDs-only
// VectorIndex contract. The legacy namespace is fixed because old callers
// cannot express namespaces, filters, or scores. New semantics are not
// fabricated or inferred.
func NewLegacyVectorIndex(public spi.VectorStore) core.VectorIndex {
	return &legacyVectorIndexAdapter{public: public, namespace: legacyNamespace}
}

type legacyVectorIndexAdapter struct {
	public    spi.VectorStore
	namespace string
}

func (a *legacyVectorIndexAdapter) Search(ctx context.Context, vector []float32, limit int) ([]string, error) {
	if a == nil || a.public == nil {
		return nil, fmt.Errorf("spiadapter: nil public vector store")
	}
	hits, err := a.public.Search(ctx, spi.SearchRequest{
		Namespace: a.namespace,
		Vector:    vector,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		ids = append(ids, hit.ID)
	}
	return ids, nil
}

func (a *legacyVectorIndexAdapter) SearchBatch(ctx context.Context, vectors [][]float32, limit int) ([][]string, error) {
	out := make([][]string, len(vectors))
	for i, vector := range vectors {
		ids, err := a.Search(ctx, vector, limit)
		if err != nil {
			return nil, err
		}
		out[i] = ids
	}
	return out, nil
}

func (a *legacyVectorIndexAdapter) Store(ctx context.Context, id string, vector []float32) error {
	if a == nil || a.public == nil {
		return fmt.Errorf("spiadapter: nil public vector store")
	}
	return a.public.Upsert(ctx, []spi.VectorRecord{{
		Namespace: a.namespace,
		ID:        id,
		Vector:    vector,
	}})
}

func (a *legacyVectorIndexAdapter) Remove(ctx context.Context, ids []string) error {
	if a == nil || a.public == nil {
		return fmt.Errorf("spiadapter: nil public vector store")
	}
	return a.public.Delete(ctx, spi.DeleteRequest{Namespace: a.namespace, IDs: ids})
}

var (
	_ spi.Extractor     = (*extractorAdapter)(nil)
	_ core.LLMExtractor = (*legacyExtractorAdapter)(nil)
	_ core.VectorIndex  = (*legacyVectorIndexAdapter)(nil)
)
