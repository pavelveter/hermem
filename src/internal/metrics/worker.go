package metrics

import (
	"database/sql"
	"log/slog"
	"sync"
	"time"
)

// AsyncMetricsWorker batches entity access tracking and flushes to DB on size or interval.
type AsyncMetricsWorker struct {
	db           *sql.DB
	ch           chan string
	done         chan struct{}
	once         sync.Once
	buffer       map[string]struct{}
	mu           sync.Mutex
	batchSize    int
	flushTimeout time.Duration
}

// NewAsyncMetricsWorker creates a new worker.
func NewAsyncMetricsWorker(db *sql.DB, bufferSize, batchSize int, flushTimeout time.Duration) *AsyncMetricsWorker {
	return &AsyncMetricsWorker{
		db: db, ch: make(chan string, bufferSize), done: make(chan struct{}),
		buffer: make(map[string]struct{}), batchSize: batchSize, flushTimeout: flushTimeout,
	}
}

// Start begins the worker loop.
func (w *AsyncMetricsWorker) Start() {
	go w.loop()
}

// Stop drains and stops the worker.
func (w *AsyncMetricsWorker) Stop() {
	w.once.Do(func() { close(w.ch); <-w.done })
}

// Touch records an entity access; drops on full channel to keep ingest path non-blocking.
func (w *AsyncMetricsWorker) Touch(entityID string) {
	select {
	case w.ch <- entityID:
	default: // drop on full channel
	}
}

func (w *AsyncMetricsWorker) loop() {
	defer close(w.done)
	ticker := time.NewTicker(w.flushTimeout)
	defer ticker.Stop()
	for {
		select {
		case id, ok := <-w.ch:
			if !ok {
				w.flush()
				return
			}
			w.mu.Lock()
			w.buffer[id] = struct{}{}
			if len(w.buffer) >= w.batchSize {
				w.flushLocked()
			}
			w.mu.Unlock()
		case <-ticker.C:
			w.mu.Lock()
			w.flushLocked()
			w.mu.Unlock()
		}
	}
}

func (w *AsyncMetricsWorker) flush() {
	w.mu.Lock()
	w.flushLocked()
	w.mu.Unlock()
}

func (w *AsyncMetricsWorker) flushLocked() {
	if len(w.buffer) == 0 {
		return
	}
	tx, err := w.db.Begin()
	if err != nil {
		slog.Warn("metrics begin tx", "err", err)
		return
	}
	for id := range w.buffer {
		if _, err := tx.Exec(`UPDATE entities SET last_accessed_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
			slog.Warn("metrics update", "id", id, "err", err)
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Warn("metrics commit", "err", err)
	}
	w.buffer = make(map[string]struct{})
}

// InitMetricsDB creates the metrics tables in the database.
func InitMetricsDB(db *sql.DB) {
	db.Exec(`CREATE TABLE IF NOT EXISTS metrics_entity_access (entity_id TEXT NOT NULL, accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_access ON metrics_entity_access(entity_id, accessed_at)`)
}

// InitMetricsWorker creates and starts a metrics worker.
func InitMetricsWorker(db *sql.DB) *AsyncMetricsWorker {
	w := NewAsyncMetricsWorker(db, 5000, 100, 100*time.Millisecond)
	w.Start()
	return w
}
