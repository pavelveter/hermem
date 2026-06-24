package algo

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// embedWork is a work item for ReEmbedAll.
type embedWork struct {
	id, content string
}

// NeedsReEmbed checks if dimension drift requires re-embedding.
func NeedsReEmbed(db *sql.DB, configuredDim int) (needs bool, oldDim int, err error) {
	err = db.QueryRow("SELECT value FROM meta WHERE key = 'embedding_dim'").Scan(&oldDim)
	if err == sql.ErrNoRows {
		return false, configuredDim, nil
	}
	if err != nil {
		return false, 0, err
	}
	return oldDim != configuredDim, oldDim, nil
}

// ReEmbedAll re-embeds all entities with the current embedder.
func ReEmbedAll(ctx context.Context, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, configuredDim int, batchSize int, modelName string) (core.ReEmbedResult, error) {
	start := time.Now()
	result := core.ReEmbedResult{NewDim: configuredDim}

	oldDim := configuredDim
	if err := db.QueryRow("SELECT value FROM meta WHERE key = 'embedding_dim'").Scan(&oldDim); err != nil && err != sql.ErrNoRows {
		return result, fmt.Errorf("read old dim: %w", err)
	}
	result.OldDim = oldDim

	if modelName != "" {
		db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('model_name', ?)", modelName)
	}

	rows, err := db.QueryContext(ctx, `SELECT id, content FROM entities WHERE archived = 0 ORDER BY id`)
	if err != nil {
		return result, fmt.Errorf("query entities: %w", err)
	}
	defer rows.Close()

	var batch []embedWork
	for rows.Next() {
		var w embedWork
		if err := rows.Scan(&w.id, &w.content); err != nil {
			return result, fmt.Errorf("scan: %w", err)
		}
		result.TotalEntities++
		if w.content == "" {
			result.Skipped++
			continue
		}
		batch = append(batch, w)
		if len(batch) >= batchSize {
			if err := processReEmbedBatch(ctx, db, vi, embedder, batch, configuredDim, &result); err != nil {
				return result, err
			}
			batch = nil
		}
	}
	if len(batch) > 0 {
		if err := processReEmbedBatch(ctx, db, vi, embedder, batch, configuredDim, &result); err != nil {
			return result, err
		}
	}

	db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('embedding_dim', ?)", fmt.Sprintf("%d", configuredDim))
	result.Elapsed = time.Since(start).Round(time.Millisecond).String()
	return result, nil
}

func processReEmbedBatch(ctx context.Context, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, items []embedWork, dim int, result *core.ReEmbedResult) error {
	result.Batches++
	for _, item := range items {
		emb, err := embedder.Embed(ctx, item.content)
		if err != nil {
			result.Failed++
			slog.Warn("re-embed failed", "id", item.id, "err", err)
			continue
		}
		if len(emb) != dim {
			result.Failed++
			slog.Warn("re-embed dim mismatch", "id", item.id, "got", len(emb), "want", dim)
			continue
		}
		vector.NormalizeVector(emb)
		blob := store.EmbeddingToBytes(emb)
		if _, err := db.ExecContext(ctx, `UPDATE entities SET embedding = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, blob, item.id); err != nil {
			result.Failed++
			slog.Warn("re-embed update", "id", item.id, "err", err)
			continue
		}
		vi.Store(ctx, item.id, emb)
		result.ReEmbedded++
	}
	return nil
}
