package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func GarbageCollector(ctx context.Context, db *sql.DB, policy RetentionPolicy) {
	if policy.RunInterval <= 0 {
		return
	}
	ticker := time.NewTicker(policy.RunInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := archiveStale(ctx, db, policy)
			if err != nil {
				slog.Error("GC cycle failed", "event", "gc_error", "err", err)
				continue
			}
			if n > 0 {
				slog.Info("GC archived stale nodes",
					"event", "gc_archived",
					"count", n,
				)
				vacuumAfter(db, n)
			}
		}
	}
}

func archiveStale(ctx context.Context, db *sql.DB, policy RetentionPolicy) (int, error) {
	if policy.ObservationTTL <= 0 {
		return 0, nil
	}

	cutoff := fmt.Sprintf("-%.0f seconds", policy.ObservationTTL.Seconds())

	query := `UPDATE entities SET archived = 1
		WHERE category = 'observation'
		AND archived = 0
		AND last_accessed_at < datetime('now', ?)`

	args := []interface{}{cutoff}

	if policy.DeleteBatchSize > 0 {
		query += fmt.Sprintf(" LIMIT %d", policy.DeleteBatchSize)
	}

	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("archive stale: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func vacuumAfter(db *sql.DB, archived int) {
	if archived <= 0 {
		return
	}
	// Incremental vacuum: free pages for the archived rows
	if _, err := db.Exec(fmt.Sprintf("PRAGMA incremental_vacuum(%d)", archived)); err != nil {
		slog.Warn("incremental vacuum failed", "event", "vacuum_error", "err", err)
	}
}

func touchAccessedBatch(db *sql.DB, ids []string) {
	if len(ids) == 0 {
		return
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	if _, err := db.Exec(fmt.Sprintf(
		"UPDATE entities SET last_accessed_at = CURRENT_TIMESTAMP WHERE id IN (%s)",
		strings.Join(placeholders, ","),
	), args...); err != nil {
		slog.Warn("touch accessed batch failed", "event", "touch_error", "err", err)
	}
}
