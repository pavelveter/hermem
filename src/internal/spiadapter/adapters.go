// Package spiadapter hosts the last remaining internal↔public contract
// bridge: the legacy ID-bearing LLM extractor (owned by the extraction
// package pending ADR-035) adapted to the identity-free spi.Extractor
// contract resolved by the public provider registry. All other bridges —
// embedder, reranker, vector store — were deleted after their callers
// migrated to pkg/spi directly.
package spiadapter

import (
	"context"
	"fmt"

	"github.com/pavelveter/hermem/pkg/domain"
	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/extraction"
)

// NewExtractor adapts the current LLM extractor result into identity-free
// public domain drafts. Prompt version and schema are retained for future
// providers but cannot be forwarded to the legacy extractor yet.
func NewExtractor(legacy extraction.LLMExtractor) spi.Extractor {
	return &extractorAdapter{legacy: legacy}
}

type extractorAdapter struct {
	legacy extraction.LLMExtractor
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

var _ spi.Extractor = (*extractorAdapter)(nil)
