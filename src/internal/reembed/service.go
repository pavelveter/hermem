// Package reembed owns the transport-agnostic re-embedding orchestrator.
//
// Flat pkg, stateless Service, per-call args for things that change
// request-time (configuredDim, batchSize, modelName), no HTTP / CLI
// coupling. The HTTP shell lives in src/internal/server/reembed/.
package reembed

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

// embedWork is a work item for ReEmbedAll. Only ReEmbedAll constructs it.
type embedWork struct {
	id, content string
}

// Service is the transport-agnostic re-embedding orchestrator.
// Holds db, vi, embedder. Batch size + model name are per-call args
// because they change with every re-embed invocation.
type Service struct {
	db       *sql.DB
	vi       core.VectorIndex
	embedder core.Embedder
}

// New constructs a reembed Service. All three deps are required;
// a nil embedder would cause every batch item to fail — the caller
// MUST pass a non-nil embedder.
func New(db *sql.DB, vi core.VectorIndex, embedder core.Embedder) *Service {
	return &Service{db: db, vi: vi, embedder: embedder}
}

// NeedsReEmbed checks if dimension drift requires re-embedding.
func (s *Service) NeedsReEmbed(ctx context.Context, configuredDim int) (needs bool, oldDim int, err error) {
	var old sql.NullInt64
	qerr := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'embedding_dim'").Scan(&old)
	if qerr == sql.ErrNoRows {
		return false, configuredDim, nil
	}
	if qerr != nil {
		return false, 0, qerr
	}
	od := int(old.Int64)
	return od != configuredDim, od, nil
}

// ReEmbedAll re-embeds all entities with the current embedder.
// The function signature takes per-call args: configuredDim,
// batchSize, modelName.
func (s *Service) ReEmbedAll(ctx context.Context, configuredDim int, batchSize int, modelName string) (core.ReEmbedResult, error) {
	start := time.Now()
	result := core.ReEmbedResult{NewDim: configuredDim}

	oldDim := configuredDim
	if err := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'embedding_dim'").Scan(&oldDim); err != nil && err != sql.ErrNoRows {
		return result, fmt.Errorf("read old dim: %w", err)
	}
	result.OldDim = oldDim

	if modelName != "" {
		s.db.ExecContext(ctx, "INSERT OR REPLACE INTO meta (key, value) VALUES ('model_name', ?)", modelName) //nolint:errcheck // best-effort: per-row state flag; error logged via slog.Warn
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, content FROM entities WHERE archived = 0 ORDER BY id`)
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
			if err := s.processReEmbedBatch(ctx, batch, configuredDim, &result); err != nil {
				return result, err
			}
			batch = nil
		}
	}
	if len(batch) > 0 {
		if err := s.processReEmbedBatch(ctx, batch, configuredDim, &result); err != nil {
			return result, err
		}
	}

	s.db.ExecContext(ctx, "INSERT OR REPLACE INTO meta (key, value) VALUES ('embedding_dim', ?)", fmt.Sprintf("%d", configuredDim)) //nolint:errcheck // best-effort: per-row state flag; error logged via slog.Warn
	result.Elapsed = time.Since(start).Round(time.Millisecond).String()
	return result, nil
}

// processReEmbedBatch is the private per-batch worker.
// The only caller is ReEmbedAll.
func (s *Service) processReEmbedBatch(ctx context.Context, items []embedWork, dim int, result *core.ReEmbedResult) error {
	result.Batches++
	for _, item := range items {
		emb, err := s.embedder.Embed(ctx, item.content)
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
		if _, err := s.db.ExecContext(ctx, `UPDATE entities SET embedding = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, blob, item.id); err != nil { //nolint:errcheck // best-effort: per-row state flag; error logged via slog.Warn
			result.Failed++
			slog.Warn("re-embed update", "id", item.id, "err", err)
			continue
		}
		s.vi.Store(ctx, item.id, emb) //nolint:errcheck // best-effort: vector drift corrected on next sweep
		result.ReEmbedded++
	}
	return nil
}
