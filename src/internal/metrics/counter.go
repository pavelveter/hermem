package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// Atomic request counters. Per-handler increments via Inc* helpers.
var (
	storeCount         atomic.Int64
	searchCount        atomic.Int64
	retrieveCount      atomic.Int64
	ingestCount        atomic.Int64
	queryCount         atomic.Int64
	edgeCount          atomic.Int64
	errorCount         atomic.Int64
	schemaConflictCount atomic.Int64
	taskStatusCount    atomic.Int64
	taskExecCount      atomic.Int64
	taskListCount      atomic.Int64
	taskShowCount      atomic.Int64
	taskDepCount       atomic.Int64
	taskRollbackCnt    atomic.Int64
	taskTreeCount      atomic.Int64
	taskCreateCnt      atomic.Int64
)

func IncStore()         { storeCount.Add(1) }
func IncSearch()        { searchCount.Add(1) }
func IncRetrieve()      { retrieveCount.Add(1) }
func IncIngest()        { ingestCount.Add(1) }
func IncQuery()         { queryCount.Add(1) }
func IncEdge()          { edgeCount.Add(1) }
func IncErr()           { errorCount.Add(1) }
// IncSchemaConflict counts 409 Schema-Conflict responses emitted by
// the cross-state tx guard (HandleStore / HandleEdge). Distinct from
// IncErr so a SIGHUP-burst of rejected writes does not pollute the
// operator's error-rate dashboard (server is healthy; clients retry).
func IncSchemaConflict() { schemaConflictCount.Add(1) }
func IncTaskStatus()   { taskStatusCount.Add(1) }
func IncTaskExec()     { taskExecCount.Add(1) }
func IncTaskList()     { taskListCount.Add(1) }
func IncTaskShow()     { taskShowCount.Add(1) }
func IncTaskDep()      { taskDepCount.Add(1) }
func IncTaskRollback() { taskRollbackCnt.Add(1) }
func IncTaskTree()     { taskTreeCount.Add(1) }
func IncTaskCreate()   { taskCreateCnt.Add(1) }

// WriteExposition writes Prometheus exposition-format metrics to w.
// Used both by MetricsHandler (HTTP) and the `hermem metrics` CLI command —
// kept as one source of truth so CLI and server output match byte-for-byte.
func WriteExposition(w io.Writer) {
	fmt.Fprintf(w, "# HELP hermem_store_total Total store operations\n# TYPE hermem_store_total counter\nhermem_store_total %d\n", storeCount.Load())
	fmt.Fprintf(w, "# HELP hermem_search_total Total search operations\n# TYPE hermem_search_total counter\nhermem_search_total %d\n", searchCount.Load())
	fmt.Fprintf(w, "# HELP hermem_retrieve_total Total retrieve operations\n# TYPE hermem_retrieve_total counter\nhermem_retrieve_total %d\n", retrieveCount.Load())
	fmt.Fprintf(w, "# HELP hermem_ingest_total Total ingest operations\n# TYPE hermem_ingest_total counter\nhermem_ingest_total %d\n", ingestCount.Load())
	fmt.Fprintf(w, "# HELP hermem_query_total Total query operations\n# TYPE hermem_query_total counter\nhermem_query_total %d\n", queryCount.Load())
	fmt.Fprintf(w, "# HELP hermem_edge_total Total edge operations\n# TYPE hermem_edge_total counter\nhermem_edge_total %d\n", edgeCount.Load())
	fmt.Fprintf(w, "# HELP hermem_errors_total Total errors\n# TYPE hermem_errors_total counter\nhermem_errors_total %d\n", errorCount.Load())
	fmt.Fprintf(w, "# HELP hermem_schema_conflict_total 409 Schema-Conflict responses from cross-state tx guard\n# TYPE hermem_schema_conflict_total counter\nhermem_schema_conflict_total %d\n", schemaConflictCount.Load())
}

// MetricsHandler serves Prometheus-format metrics.
func MetricsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	WriteExposition(w)
}
