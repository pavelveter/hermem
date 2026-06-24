package algo

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// GarbageCollector periodically archives stale observation nodes per policy.
func GarbageCollector(ctx context.Context, db *sql.DB, vi core.VectorIndex, policy core.RetentionPolicy) {
	ticker := time.NewTicker(policy.RunInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-policy.ObservationTTL)
			rows, err := db.QueryContext(ctx, `SELECT id FROM entities WHERE category = 'observation' AND updated_at < ? AND archived = 0 LIMIT ?`, cutoff, policy.DeleteBatchSize)
			if err != nil {
				slog.Error("gc query", "err", err)
				continue
			}
			var ids []string
			for rows.Next() {
				var id string
				rows.Scan(&id)
				ids = append(ids, id)
			}
			rows.Close()
			if len(ids) == 0 {
				continue
			}
			tx, _ := db.BeginTx(ctx, nil)
			for _, id := range ids {
				if _, err := tx.ExecContext(ctx, `UPDATE entities SET archived = 1 WHERE id = ?`, id); err != nil {
					slog.Warn("gc archive", "id", id, "err", err)
				}
			}
			if err := tx.Commit(); err != nil {
				slog.Warn("gc commit", "err", err)
				continue
			}
			vi.Remove(ctx, ids)
			slog.Info("gc archived", "count", len(ids))
		}
	}
}
