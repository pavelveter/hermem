// Package metrics holds the dependency-injected request counters + the
// Prometheus exposition writer.
//
// State model: every counter is a field of the Metrics struct (atomic.Int64).
// The Metrics struct is constructed once per process via metrics.New() and
// threaded through clienv.Env → server service constructors → HTTP handlers.
// This replaces the round-1 through round-8 package-level atomic.Int64
// globals — removes the cross-test pollution surface area and makes the
// counter wiring unit-testable without lockstep cleanup between tests.
//
// async-metrics (the per-entity last_accessed_at tracking) is owned by a
// separate concern: AsyncMetricsWorker (worker.go). It batches writes to
// the metrics_entity_access SQLite table — a different state model
// (DB-backed, not in-process atomic) — and is NOT refactored in this PR.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// Metrics holds the Prometheus-style request counters as atomic.Int64 fields.
// Methods Inc* increment and WriteExposition emits the canonical exposition
// text format; MetricsHandler is the HTTP wrapper.
//
// All fields are private to keep the increment surface (the 16 Inc* methods
// + WriteExposition) the only call paths; callers cannot accidentally bypass
// the atomic increment by direct field access.
type Metrics struct {
	storeCount          atomic.Int64
	searchCount         atomic.Int64
	retrieveCount       atomic.Int64
	ingestCount         atomic.Int64
	queryCount          atomic.Int64
	edgeCount           atomic.Int64
	errorCount          atomic.Int64
	schemaConflictCount atomic.Int64
	taskStatusCount     atomic.Int64
	taskExecCount       atomic.Int64
	taskListCount       atomic.Int64
	taskShowCount       atomic.Int64
	taskDepCount        atomic.Int64
	taskRollbackCount   atomic.Int64
	taskTreeCount       atomic.Int64
	taskCreateCount     atomic.Int64
}

// New returns a fresh Metrics instance with all counters zero-initialised.
// Each process / test should call New exactly once and pass it through the
// service constructors — never share one *Metrics between two test bodies,
// or cross-test increments will leak.
func New() *Metrics {
	return &Metrics{}
}

// --- Increment methods ---
//
// Each handler-side IncXxx performs a single atomic.Add(1) on its dedicated
// counter. Atomic.Int64 is the lowest-level thread-safe increment available
// in Go's stdlib (no mutex, no CAS retry spin under contention) — chosen
// over a Mutex+int pair because (a) handlers are request-hot paths, (b)
// IncXxx is one-line mem-to-mem, (c) Prometheus exposition format requires
// Load() to return an exact snapshot, which an int64 mutex-guarded counter
// cannot promise without complicating the handler shape.

func (m *Metrics) IncStore()    { m.storeCount.Add(1) }
func (m *Metrics) IncSearch()   { m.searchCount.Add(1) }
func (m *Metrics) IncRetrieve() { m.retrieveCount.Add(1) }
func (m *Metrics) IncIngest()   { m.ingestCount.Add(1) }
func (m *Metrics) IncQuery()    { m.queryCount.Add(1) }
func (m *Metrics) IncEdge()     { m.edgeCount.Add(1) }
func (m *Metrics) IncErr()      { m.errorCount.Add(1) }

// IncSchemaConflict counts 409 Schema-Conflict responses emitted by
// the cross-state tx guard (HandleStore / HandleEdge). Distinct from
// IncErr so a SIGHUP-burst of rejected writes does not pollute the
// operator's error-rate dashboard (server is healthy; clients retry).
func (m *Metrics) IncSchemaConflict() { m.schemaConflictCount.Add(1) }
func (m *Metrics) IncTaskStatus()     { m.taskStatusCount.Add(1) }
func (m *Metrics) IncTaskExec()       { m.taskExecCount.Add(1) }
func (m *Metrics) IncTaskList()       { m.taskListCount.Add(1) }
func (m *Metrics) IncTaskShow()       { m.taskShowCount.Add(1) }
func (m *Metrics) IncTaskDep()        { m.taskDepCount.Add(1) }
func (m *Metrics) IncTaskRollback()   { m.taskRollbackCount.Add(1) }
func (m *Metrics) IncTaskTree()       { m.taskTreeCount.Add(1) }
func (m *Metrics) IncTaskCreate()     { m.taskCreateCount.Add(1) }

// WriteExposition writes Prometheus exposition-format metrics to w.
// Used by both the HTTP handler (`MetricsHandler`) and the
// `hermem metrics` CLI command — kept as one source of truth so
// CLI and server output match byte-for-byte.
//
// Format spec: https://prometheus.io/docs/instrumenting/exposition_formats/
// — each metric emits a HELP line, a TYPE line, and a value line,
// in that order. The order of `hermem_*_total` records is fixed
// (insertion order of the Inc* calls above) so a scraper diff
// against a previous scrape changes only the numeric field.
func (m *Metrics) WriteExposition(w io.Writer) {
	fmt.Fprintf(w, "# HELP hermem_store_total Total store operations\n# TYPE hermem_store_total counter\nhermem_store_total %d\n", m.storeCount.Load())
	fmt.Fprintf(w, "# HELP hermem_search_total Total search operations\n# TYPE hermem_search_total counter\nhermem_search_total %d\n", m.searchCount.Load())
	fmt.Fprintf(w, "# HELP hermem_retrieve_total Total retrieve operations\n# TYPE hermem_retrieve_total counter\nhermem_retrieve_total %d\n", m.retrieveCount.Load())
	fmt.Fprintf(w, "# HELP hermem_ingest_total Total ingest operations\n# TYPE hermem_ingest_total counter\nhermem_ingest_total %d\n", m.ingestCount.Load())
	fmt.Fprintf(w, "# HELP hermem_query_total Total query operations\n# TYPE hermem_query_total counter\nhermem_query_total %d\n", m.queryCount.Load())
	fmt.Fprintf(w, "# HELP hermem_edge_total Total edge operations\n# TYPE hermem_edge_total counter\nhermem_edge_total %d\n", m.edgeCount.Load())
	fmt.Fprintf(w, "# HELP hermem_errors_total Total errors\n# TYPE hermem_errors_total counter\nhermem_errors_total %d\n", m.errorCount.Load())
	fmt.Fprintf(w, "# HELP hermem_schema_conflict_total 409 Schema-Conflict responses from cross-state tx guard\n# TYPE hermem_schema_conflict_total counter\nhermem_schema_conflict_total %d\n", m.schemaConflictCount.Load())
}

// MetricsHandler serves Prometheus-format metrics over HTTP.
func (m *Metrics) MetricsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	m.WriteExposition(w)
}
