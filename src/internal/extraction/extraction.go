// Package extraction owns the legacy ID-bearing LLM extraction contract
// consumed by the ingestion, compression, and contradiction pipelines.
//
// The public spi.Extractor contract is deliberately identity-free; this
// legacy shape preserves the model-suggested entity IDs those pipelines
// persist today. ADR-035 owns the identity strategy that retires it.
package extraction

import (
	"context"

	"github.com/pavelveter/hermem/pkg/domain"
)

// LLMExtractor runs entity+relation extraction on a dialog and returns
// the legacy result whose entities carry model-suggested IDs.
type LLMExtractor interface {
	ExtractEntities(ctx context.Context, dialog string) (*domain.ExtractionResult, error)
}
