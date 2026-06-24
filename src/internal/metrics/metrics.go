// Package metrics provides async metrics collection and Prometheus endpoint.
package metrics

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// AsyncMetricsWorker batches entity access tracking and flushes to DB.
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

// Touch records an entity access.
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
		tx.Exec(`UPDATE entities SET last_accessed_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	}
	tx.Commit()
	w.buffer = make(map[string]struct{})
}

// --- Counter helpers ---

var (
	storeCount      atomic.Int64
	searchCount     atomic.Int64
	retrieveCount   atomic.Int64
	ingestCount     atomic.Int64
	queryCount      atomic.Int64
	edgeCount       atomic.Int64
	errorCount      atomic.Int64
	taskStatusCount atomic.Int64
	taskExecCount   atomic.Int64
	taskListCount   atomic.Int64
	taskShowCount   atomic.Int64
	taskDepCount    atomic.Int64
	taskRollbackCnt atomic.Int64
	taskTreeCount   atomic.Int64
	taskCreateCnt   atomic.Int64
)

func IncStore()        { storeCount.Add(1) }
func IncSearch()       { searchCount.Add(1) }
func IncRetrieve()     { retrieveCount.Add(1) }
func IncIngest()       { ingestCount.Add(1) }
func IncQuery()        { queryCount.Add(1) }
func IncEdge()         { edgeCount.Add(1) }
func IncErr()          { errorCount.Add(1) }
func IncTaskStatus()   { taskStatusCount.Add(1) }
func IncTaskExec()     { taskExecCount.Add(1) }
func IncTaskList()     { taskListCount.Add(1) }
func IncTaskShow()     { taskShowCount.Add(1) }
func IncTaskDep()      { taskDepCount.Add(1) }
func IncTaskRollback() { taskRollbackCnt.Add(1) }
func IncTaskTree()     { taskTreeCount.Add(1) }
func IncTaskCreate()   { taskCreateCnt.Add(1) }

// MetricsHandler serves Prometheus-format metrics.
func MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP hermem_store_total Total store operations\n# TYPE hermem_store_total counter\nhermem_store_total %d\n", storeCount.Load())
	fmt.Fprintf(w, "# HELP hermem_search_total Total search operations\n# TYPE hermem_search_total counter\nhermem_search_total %d\n", searchCount.Load())
	fmt.Fprintf(w, "# HELP hermem_retrieve_total Total retrieve operations\n# TYPE hermem_retrieve_total counter\nhermem_retrieve_total %d\n", retrieveCount.Load())
	fmt.Fprintf(w, "# HELP hermem_ingest_total Total ingest operations\n# TYPE hermem_ingest_total counter\nhermem_ingest_total %d\n", ingestCount.Load())
	fmt.Fprintf(w, "# HELP hermem_query_total Total query operations\n# TYPE hermem_query_total counter\nhermem_query_total %d\n", queryCount.Load())
	fmt.Fprintf(w, "# HELP hermem_edge_total Total edge operations\n# TYPE hermem_edge_total counter\nhermem_edge_total %d\n", edgeCount.Load())
	fmt.Fprintf(w, "# HELP hermem_errors_total Total errors\n# TYPE hermem_errors_total counter\nhermem_errors_total %d\n", errorCount.Load())
}

// InitMetricsDB creates the metrics tables in the database.
func InitMetricsDB(db *sql.DB) {
	db.Exec(`CREATE TABLE IF NOT EXISTS metrics_entity_access (entity_id TEXT NOT NULL, accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_access ON metrics_entity_access(entity_id, accessed_at)`)
}

// MetricsWorker is the package-level worker reference (set by main).
var MetricsWorker *AsyncMetricsWorker

// InitMetricsWorker creates and starts a metrics worker.
func InitMetricsWorker(db *sql.DB) *AsyncMetricsWorker {
	w := NewAsyncMetricsWorker(db, 5000, 100, 100*time.Millisecond)
	w.Start()
	MetricsWorker = w
	return w
}
