package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// ReEmbedResult holds the outcome of a background re-embedding run.
type ReEmbedResult struct {
	TotalEntities int    `json:"total_entities"`
	ReEmbedded    int    `json:"re_embedded"`
	Skipped       int    `json:"skipped"` // entities without content
	Failed        int    `json:"failed"`  // embed calls that errored
	Elapsed       string `json:"elapsed"`
	OldDim        int    `json:"old_dim"`
	NewDim        int    `json:"new_dim"`
	Batches       int    `json:"batches"`
}

// NeedsReEmbed checks if the stored embedding dimension in meta
// matches the configured VectorDim. Returns (needsReEmbed, oldDim, nil).
// A fresh database (no meta row) returns false.
func NeedsReEmbed(db *sql.DB, configuredDim int) (bool, int, error) {
	var storedDim int
	err := db.QueryRow("SELECT value FROM meta WHERE key = 'embedding_dim'").Scan(&storedDim)
	if err == sql.ErrNoRows {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("check embedding dim: %w", err)
	}
	if storedDim != configuredDim && storedDim != 0 {
		return true, storedDim, nil
	}
	return false, storedDim, nil
}

// ReEmbedAll re-embeds every non-archived entity that has content,
// storing the updated embedding in both the entities table and the
// vector index. Batch size controls how many entities are re-embedded
// within each DB transaction. Logs progress every batch.
//
// On completion it updates meta.embedding_dim to the new dimension.
func ReEmbedAll(ctx context.Context, db *sql.DB, vi VectorIndex, embedder Embedder, configuredDim int, batchSize int, modelName string) (*ReEmbedResult, error) {
	if batchSize <= 0 {
		batchSize = 50
	}

	start := time.Now()

	// Count total entities and empty-content entities.
	var total, emptyContent int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities WHERE archived = 0`).Scan(&total); err != nil {
		return nil, fmt.Errorf("count entities: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entities WHERE archived = 0 AND content = ''`).Scan(&emptyContent); err != nil {
		return nil, fmt.Errorf("count empty entities: %w", err)
	}

	result := &ReEmbedResult{
		TotalEntities: total,
		Skipped:       emptyContent,
		NewDim:        configuredDim,
	}

	// Look up stored dimension for reporting.
	var oldDim int
	if err := db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'embedding_dim'").Scan(&oldDim); err != nil {
		oldDim = 0
	}

	result.OldDim = oldDim

	slog.Info("re-embed started",
		"event", "reembed_start",
		"total", total,
		"batch_size", batchSize,
		"old_dim", oldDim,
		"new_dim", configuredDim,
	)

	// Chunk through entities with content.
	reembeddable := total - emptyContent
	offset := 0
	for offset < reembeddable {
		batchStart := time.Now()

		rows, err := db.QueryContext(ctx, `
			SELECT id, content FROM entities
			WHERE archived = 0 AND content != ''
			ORDER BY id
			LIMIT ? OFFSET ?
		`, batchSize, offset)
		if err != nil {
			return nil, fmt.Errorf("query entities batch %d: %w", offset/batchSize, err)
		}

		type row struct {
			id, content string
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.content); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan entity: %w", err)
			}
			batch = append(batch, r)
		}
		rows.Close()

		// Embed each entity.
		type reembed struct {
			id  string
			vec []float32
		}
		var embeddings []reembed
		for _, r := range batch {
			vec, err := embedder.Embed(ctx, r.content)
			if err != nil {
				slog.Warn("re-embed: embed failed, skipping entity",
					"event", "reembed_entity_fail",
					"id", r.id,
					"error", err,
				)
				result.Failed++
				continue
			}
			// Guard against dimension drift from the embedder.
			if len(vec) != configuredDim {
				slog.Warn("re-embed: dimension mismatch, skipping entity",
					"event", "reembed_dim_mismatch",
					"id", r.id,
					"got_dim", len(vec),
					"want_dim", configuredDim,
				)
				result.Failed++
				continue
			}
			NormalizeVector(vec)
			embeddings = append(embeddings, reembed{id: r.id, vec: vec})
		}

		// Write to DB and vector index in batch.
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("begin tx batch %d: %w", offset/batchSize, err)
		}

		for _, e := range embeddings {
			blob := EmbeddingToBytes(e.vec)
			if _, err := tx.ExecContext(ctx,
				`UPDATE entities SET embedding = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				blob, e.id,
			); err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("update entity %s: %w", e.id, err)
			}

			if err := vi.Store(ctx, e.id, e.vec); err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("update vector index %s: %w", e.id, err)
			}

			result.ReEmbedded++
		}

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit batch %d: %w", offset/batchSize, err)
		}

		result.Batches++

		slog.Info("re-embed batch",
			"event", "reembed_batch",
			"batch", result.Batches,
			"entities", len(embeddings),
			"duration", time.Since(batchStart).String(),
			"		progress", fmt.Sprintf("%d/%d", result.ReEmbedded, reembeddable),
		)

		offset += batchSize

		// Check context cancellation between batches.
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
	}

	// Update meta.
	if _, err := db.ExecContext(ctx,
		`INSERT OR REPLACE INTO meta (key, value) VALUES ('embedding_dim', ?)`,
		fmt.Sprintf("%d", configuredDim),
	); err != nil {
		return nil, fmt.Errorf("update meta.embedding_dim: %w", err)
	}
	if modelName != "" {
		if _, err := db.ExecContext(ctx,
			`INSERT OR REPLACE INTO meta (key, value) VALUES ('model_name', ?)`,
			modelName,
		); err != nil {
			slog.Warn("re-embed: failed to update model_name", "error", err)
		}
	}

	result.Elapsed = time.Since(start).Round(time.Millisecond).String()
	result.Skipped = emptyContent // entities without content were skipped from the start

	slog.Info("re-embed complete",
		"event", "reembed_complete",
		"total", result.TotalEntities,
		"re_embedded", result.ReEmbedded,
		"failed", result.Failed,
		"batches", result.Batches,
		"elapsed", result.Elapsed,
	)

	return result, nil
}
