// Package core defines the foundational domain types shared across all hermem packages.
// It has zero internal dependencies and is imported by every other package.
//
// Deprecated: the facade is being removed in the breaking release. Value
// types whose canonical home is pkg/domain are now aliases; new code must
// import pkg/domain directly.
package core

import (
	"context"

	"github.com/pavelveter/hermem/pkg/spi"
)

// VectorIndex is the interface for vector similarity search and storage.
type VectorIndex interface {
	Search(ctx context.Context, vec []float32, limit int) ([]string, error)
	SearchBatch(ctx context.Context, vecs [][]float32, limit int) ([][]string, error)
	Store(ctx context.Context, id string, vec []float32) error
	Remove(ctx context.Context, ids []string) error
}

// Embedder converts text to a float32 embedding vector.
//
// Deprecated: alias to the canonical pkg/spi.Embedder. Health checks use
// an optional spi.Pinger assertion instead of a required Ping method.
type Embedder = spi.Embedder
