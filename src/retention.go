package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

func GarbageCollector(ctx context.Context, db *sql.DB, vi VectorIndex, policy RetentionPolicy) {
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
			n, err := archiveStale(ctx, db, vi, policy)
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

func archiveStale(ctx context.Context, db *sql.DB, vi VectorIndex, policy RetentionPolicy) (int, error) {
	if policy.ObservationTTL <= 0 {
		return 0, nil
	}

	cutoff := fmt.Sprintf("-%.0f seconds", policy.ObservationTTL.Seconds())

	selectQuery := `SELECT id FROM entities
		WHERE category = 'observation'
		AND archived = 0
		AND last_accessed_at < datetime('now', ?)`

	args := []interface{}{cutoff}

	if policy.DeleteBatchSize > 0 {
		selectQuery += fmt.Sprintf(" LIMIT %d", policy.DeleteBatchSize)
	}

	rows, err := db.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return 0, fmt.Errorf("select stale: %w", err)
	}

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan stale id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate stale ids: %w", err)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	// Mark as archived in SQLite
	phs, args := inClauseArgs(ids)
	_, err = db.ExecContext(ctx, fmt.Sprintf(
		"UPDATE entities SET archived = 1 WHERE id IN (%s)", phs), args...)
	if err != nil {
		return 0, fmt.Errorf("archive stale: %w", err)
	}

	// Evict from in-memory vector index
	if err := vi.Remove(ctx, ids); err != nil {
		slog.Warn("vector index remove failed",
			"event", "gc_vector_remove_error",
			"err", err,
			"count", len(ids),
		)
	}

	return len(ids), nil
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
