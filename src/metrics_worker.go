package main

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"
)

type AsyncMetricsWorker struct {
	db           *sql.DB
	ch           chan string
	batchSize    int
	flushTimeout time.Duration
	stopCh       chan struct{}
}

func NewAsyncMetricsWorker(db *sql.DB, bufferSize, batchSize int, flushTimeout time.Duration) *AsyncMetricsWorker {
	return &AsyncMetricsWorker{
		db:           db,
		ch:           make(chan string, bufferSize),
		batchSize:    batchSize,
		flushTimeout: flushTimeout,
		stopCh:       make(chan struct{}),
	}
}

func (w *AsyncMetricsWorker) Start() {
	go func() {
		ticker := time.NewTicker(w.flushTimeout)
		defer ticker.Stop()

		pending := make(map[string]struct{})

		flush := func() {
			if len(pending) == 0 {
				return
			}
			ids := make([]string, 0, len(pending))
			for id := range pending {
				ids = append(ids, id)
			}
			if err := flushAccessedBatch(context.Background(), w.db, ids); err != nil {
				slog.Error("async_metrics_flush_failed",
					"event", "metrics_flush_error",
					"error", err,
					"count", len(ids),
				)
			}
			pending = make(map[string]struct{}, w.batchSize)
		}

		for {
			select {
			case id, ok := <-w.ch:
				if !ok {
					flush()
					return
				}
				pending[id] = struct{}{}
				if len(pending) >= w.batchSize {
					flush()
				}

			case <-ticker.C:
				flush()

			case <-w.stopCh:
				flush()
				return
			}
		}
	}()
}

func (w *AsyncMetricsWorker) Touch(id string) {
	if w == nil {
		return
	}
	select {
	case w.ch <- id:
	default:
		slog.Warn("metrics_channel_full_dropping_event",
			"event", "metrics_dropped",
			"id", id,
		)
	}
}

func (w *AsyncMetricsWorker) Stop() {
	if w == nil {
		return
	}
	close(w.stopCh)
}

func flushAccessedBatch(ctx context.Context, db *sql.DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	_, err := db.ExecContext(ctx,
		"UPDATE entities SET last_accessed_at = CURRENT_TIMESTAMP WHERE id IN ("+
			strings.Join(placeholders, ",")+")", args...)
	return err
}
