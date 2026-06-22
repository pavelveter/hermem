package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const maxSQLiteVars = 999

type AsyncMetricsWorker struct {
	db           *sql.DB
	ch           chan string
	batchSize    int
	flushTimeout time.Duration
	stopCh       chan struct{}
	flushReqCh   chan chan struct{}
	wg           sync.WaitGroup
	onceStop     sync.Once
}

func NewAsyncMetricsWorker(db *sql.DB, bufferSize, batchSize int, flushTimeout time.Duration) *AsyncMetricsWorker {
	return &AsyncMetricsWorker{
		db:           db,
		ch:           make(chan string, bufferSize),
		batchSize:    batchSize,
		flushTimeout: flushTimeout,
		stopCh:       make(chan struct{}),
		flushReqCh:   make(chan chan struct{}),
	}
}

func (w *AsyncMetricsWorker) Start() {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
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
			for i := 0; i < len(ids); i += maxSQLiteVars {
				end := i + maxSQLiteVars
				if end > len(ids) {
					end = len(ids)
				}
				if err := flushAccessedBatch(context.Background(), w.db, ids[i:end]); err != nil {
					slog.Error("async_metrics_flush_failed",
						"event", "metrics_flush_error",
						"error", err,
						"count", len(ids[i:end]),
					)
				}
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

			case replyCh := <-w.flushReqCh:
				drain := true
				for drain {
					select {
					case id, ok := <-w.ch:
						if !ok {
							drain = false
						} else {
							pending[id] = struct{}{}
						}
					default:
						drain = false
					}
				}
				flush()
				close(replyCh)

			case <-w.stopCh:
				for len(w.ch) > 0 {
					if id, ok := <-w.ch; ok {
						pending[id] = struct{}{}
					}
				}
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

func (w *AsyncMetricsWorker) Flush() {
	if w == nil {
		return
	}
	replyCh := make(chan struct{})
	select {
	case w.flushReqCh <- replyCh:
		<-replyCh
	case <-w.stopCh:
	}
}

func (w *AsyncMetricsWorker) Stop() {
	w.onceStop.Do(func() {
		close(w.stopCh)
		w.wg.Wait()
	})
}

func flushAccessedBatch(ctx context.Context, db *sql.DB, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	phs, args := inClauseArgs(ids)
	_, err := db.ExecContext(ctx,
		"UPDATE entities SET last_accessed_at = CURRENT_TIMESTAMP WHERE id IN ("+phs+")", args...)
	if err != nil {
		return fmt.Errorf("bulk update entities: %w", err)
	}
	return nil
}
