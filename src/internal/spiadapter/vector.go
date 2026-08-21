package spiadapter

import (
	"context"

	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/core"
)

// VectorStore accepts the canonical public contract and, during the
// compatibility release, legacy core.VectorIndex values used by tests and
// callers that have not migrated yet. Production wiring should pass a
// spi.VectorStore directly.
func VectorStore(value any) spi.VectorStore {
	switch v := value.(type) {
	case nil:
		return nil
	case spi.VectorStore:
		return v
	case core.VectorIndex:
		return NewVectorStore(v)
	case interface {
		Store(context.Context, string, []float32) error
		Remove(context.Context, []string) error
	}:
		return NewWriteOnlyVectorStore(v)
	default:
		return nil
	}
}

// NewWriteOnlyVectorStore adapts a legacy write-only index for admin
// operations. Search remains explicitly unsupported.
func NewWriteOnlyVectorStore(legacy interface {
	Store(context.Context, string, []float32) error
	Remove(context.Context, []string) error
}) spi.VectorStore {
	return &writeOnlyVectorStore{legacy: legacy}
}

type writeOnlyVectorStore struct {
	legacy interface {
		Store(context.Context, string, []float32) error
		Remove(context.Context, []string) error
	}
}

func (a *writeOnlyVectorStore) Search(context.Context, spi.SearchRequest) ([]spi.Hit, error) {
	return nil, spi.ErrUnsupported
}
func (a *writeOnlyVectorStore) Upsert(ctx context.Context, records []spi.VectorRecord) error {
	for _, record := range records {
		if err := a.legacy.Store(ctx, record.ID, record.Vector); err != nil {
			return err
		}
	}
	return nil
}
func (a *writeOnlyVectorStore) Delete(ctx context.Context, req spi.DeleteRequest) error {
	return a.legacy.Remove(ctx, req.IDs)
}
func (a *writeOnlyVectorStore) Stats(context.Context, string) (spi.VectorStats, error) {
	return spi.VectorStats{}, spi.ErrUnsupported
}

// NewVectorStore adapts a legacy index to the public contract. This is a
// temporary boundary; it preserves IDs-only behavior and therefore cannot
// provide meaningful filters or backend-native namespaces.
func NewVectorStore(legacy core.VectorIndex) spi.VectorStore {
	return &legacyToPublicVectorStore{legacy: legacy}
}

type legacyToPublicVectorStore struct {
	legacy core.VectorIndex
}

func (a *legacyToPublicVectorStore) SearchBatch(ctx context.Context, vectors [][]float32, limit int) ([][]string, error) {
	return a.legacy.SearchBatch(ctx, vectors, limit)
}

func (a *legacyToPublicVectorStore) Search(ctx context.Context, req spi.SearchRequest) ([]spi.Hit, error) {
	ids, err := a.legacy.Search(ctx, req.Vector, req.Limit)
	if err != nil {
		return nil, err
	}
	hits := make([]spi.Hit, 0, len(ids))
	for _, id := range ids {
		hits = append(hits, spi.Hit{ID: id})
	}
	return hits, nil
}

func (a *legacyToPublicVectorStore) Upsert(ctx context.Context, records []spi.VectorRecord) error {
	for _, record := range records {
		if err := a.legacy.Store(ctx, record.ID, record.Vector); err != nil {
			return err
		}
	}
	return nil
}

func (a *legacyToPublicVectorStore) Delete(ctx context.Context, req spi.DeleteRequest) error {
	return a.legacy.Remove(ctx, req.IDs)
}

func (a *legacyToPublicVectorStore) Stats(context.Context, string) (spi.VectorStats, error) {
	return spi.VectorStats{}, spi.ErrUnsupported
}

var _ spi.VectorStore = (*legacyToPublicVectorStore)(nil)
