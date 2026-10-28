package vector

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/store"
)

// warmBatchSize bounds the number of vectors handed to a single Upsert so a
// large knowledge base does not build one giant slice in memory.
const warmBatchSize = 512

// Warm backfills a process-local VectorStore from the embeddings already
// persisted in `entities`.
//
// The in-memory backend is the only implemented one (the sqlite-vec engine is
// still a stub), and it starts empty on every process start. Without this
// warm-up a fresh CLI invocation, MCP server, or daemon can only search the
// rows it inserted itself: `entities.embedding` holds the vectors, but
// SearchByVector asks the VectorStore for candidate IDs first.
//
// Persistence-backed backends already own their index and are left untouched.
// Rows whose blob is missing, of an unexpected dimension, or non-finite are
// skipped — one corrupt row must not make the whole knowledge base
// unsearchable — and reported through the returned skipped count.
//
// Returns the number of vectors loaded.
func Warm(ctx context.Context, db *sql.DB, vi spi.VectorStore, dim int) (loaded int, skipped int, err error) {
	if db == nil || vi == nil {
		return 0, 0, fmt.Errorf("warm: nil db or vector store")
	}
	if _, ok := vi.(*inMemoryVectorStore); !ok {
		return 0, 0, nil
	}
	if dim <= 0 {
		return 0, 0, fmt.Errorf("warm: vector dim must be positive, got %d", dim)
	}

	rows, err := db.QueryContext(ctx, `SELECT id, embedding FROM entities WHERE archived = 0 AND embedding IS NOT NULL AND length(embedding) > 0`)
	if err != nil {
		return 0, 0, fmt.Errorf("warm: query embeddings: %w", err)
	}
	defer rows.Close()

	batch := make([]spi.VectorRecord, 0, warmBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := vi.Upsert(ctx, batch); err != nil {
			return fmt.Errorf("warm: upsert %d vectors: %w", len(batch), err)
		}
		loaded += len(batch)
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return loaded, skipped, fmt.Errorf("warm: scan row: %w", err)
		}
		vec, decErr := store.DecodeVector(blob, dim)
		if decErr != nil {
			slog.Warn("warm: skipping entity with unusable embedding", "event", "warm_skip", "entity_id", id, "err", decErr)
			skipped++
			continue
		}
		batch = append(batch, spi.VectorRecord{Namespace: spi.DefaultNamespace, ID: id, Vector: vec})
		if len(batch) == warmBatchSize {
			if err := flush(); err != nil {
				return loaded, skipped, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return loaded, skipped, fmt.Errorf("warm: iterate embeddings: %w", err)
	}
	if err := flush(); err != nil {
		return loaded, skipped, err
	}
	return loaded, skipped, nil
}
