// Package metrics owns hermem_* Prometheus metrics and exposes a single
// hermem-owned *prometheus.Registry the HTTP layer can serve via promhttp.
//
// OBSERVABILITY sprint commits 1-2 added a Prometheus driver layer underneath
// the original Metrics struct (atomic.Int64 + IncXxx methods preserved
// byte-compatibly from e2aa722). Commit 1 (0195a23) wired 17 IncXxx counters
// (atomic + prometheus.Counter in lockstep). Commit 2 added 4 duration
// histograms (ingest/retrieval/contradiction/rerank) for LLM-driven latency
// visibility; histograms are prometheus-native only (no atomic dual-track —
// bucket layout is too complex for the legacy text-format path).
//
// IMPORTANT (called out for future maintainers): per-domain collectors
// added by commits 3-6 (HistogramVecs for category/mode-tagged latency,
// GaugeVecs for graph depth, vec counters tagged by detector, etc.) all
// register on the SAME *prometheus.Registry returned by PrometheusRegistry().
// Do NOT call prometheus.MustRegister / promauto — that pins to the global
// default and silently defeats the hermem-owned intent.
//
// Histograms added in commit 2 use 10 hand-picked buckets sized for hermem's
// bimodal latency profile (sub-100ms embedding lookups vs 2-60s LLM extraction):
//   .05 .1 .5 1 2 5 10 15 30 60 (seconds)
// Future commits adjust bucket ranges per the new command distribution once
// the /metrics endpoint ships and is scraped by a Prometheus instance.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// durationBuckets is shared by all 4 histograms added in commit 2.
// Sized for hermem's bimodal latency: fast embeddings (~50ms) vs slow LLM
// extraction (2-60s). Prometheus quantiles can interpolate but cannot
// extrapolate — the highest bucket matters: anything > 60s lands in +Inf.
//
// Future commits may override at construction time if the production
// latency distribution shifts; do not extend this slice ad-hoc whenever
// a new histogram domain appears.
var durationBuckets = []float64{0.05, 0.1, 0.5, 1, 2, 5, 10, 15, 30, 60}

// Metrics is the hermem-wide counter + histogram bag.
//
// Migration cheat-sheet (when adding a new IncXxx counter):
//
// A maintainer adding e.g. IncConnect must edit 7 spots in this file, in order:
//
//	1. Struct atomic field — add `connectCount atomic.Int64` to the atomic block.
//	2. Struct prom field — add `pConnect prometheus.Counter` to the prom block.
//	3. New() init — construct m.pConnect = prometheus.NewCounter(CounterOpts{Name: "hermem_connect_total", Help: "..."})
//	4. New() MustRegister — append m.pConnect to the vararg list.
//	5. IncXxx method body — `m.connectCount.Add(1); m.pConnect.Inc()`
//	6. WriteExposition line — one fmt.Fprintf("# HELP hermem_connect_total …\n# TYPE …\nhermem_connect_total %d\n", m.connectCount.Load())
//	7. metrics_test.go — add to cases slice + wantProm slice; run go test.
//
// Migration cheat-sheet (when adding a new duration histogram, e.g. hExport):
//
//	1. Struct field — add `hExport prometheus.Histogram` to the histograms block.
//	2. New() init — m.hExport = prometheus.NewHistogram(HistogramOpts{Name: "hermem_export_duration_seconds", Help: "...", Buckets: durationBuckets}).
//	3. New() MustRegister — append m.hExport to the vararg list (or add to a second MustRegister call alongside the counters).
//	4. Observe method — `func (m *Metrics) ObserveExportDuration(seconds float64) { m.hExport.Observe(seconds) }`.
//	5. metrics_test.go — extend TestDurationHistograms to call the new Observe method and assert count+sum.
//
// Future OBSERVABILITY commits (3-6) will upgrade individual hIngest / hRetrieval
// / hContradiction / hRerank fields to *prometheus.HistogramVec to add a single
// label each (category / mode / detector / strategy). The Observe method
// signatures will change to accept a label string at that point. New callers
// must follow the new signature; pre-2 callers will break intentionally.
type Metrics struct {
	// atomic.Int64 fields preserved from e2aa722 verbatim — keep server-side
	// callers byte-compatible (pre-OBSERVABILITY code used these names).
	storeCount          atomic.Int64
	searchCount         atomic.Int64
	retrieveCount       atomic.Int64
	ingestCount         atomic.Int64
	queryCount          atomic.Int64
	edgeCount           atomic.Int64
	errCount            atomic.Int64
	schemaConflictCount atomic.Int64
	taskStatusCount     atomic.Int64
	taskExecCount       atomic.Int64
	taskListCount       atomic.Int64
	taskShowCount       atomic.Int64
	taskDepCount        atomic.Int64
	taskRollbackCount   atomic.Int64
	taskTreeCount       atomic.Int64
	taskCreateCount     atomic.Int64
	retentionRunCount   atomic.Int64

	// Prometheus counters (added by OBSERVABILITY commit 1/8).
	promReg         *prometheus.Registry
	pStore          prometheus.Counter
	pSearch         prometheus.Counter
	pRetrieve       prometheus.Counter
	pIngest         prometheus.Counter
	pQuery          prometheus.Counter
	pEdge           prometheus.Counter
	pErr            prometheus.Counter
	pSchemaConflict prometheus.Counter
	pTaskStatus     prometheus.Counter
	pTaskExec       prometheus.Counter
	pTaskList       prometheus.Counter
	pTaskShow       prometheus.Counter
	pTaskDep        prometheus.Counter
	pTaskRollback   prometheus.Counter
	pTaskTree       prometheus.Counter
	pTaskCreate     prometheus.Counter
	pRetentionRun   prometheus.Counter

	// Prometheus histograms (added by OBSERVABILITY commit 2/8).
	// Single histograms (no labels) — commit 3 upgrades hIngest to a
	// *HistogramVec labelled by category; commit 4 upgrades hRetrieval
	// for mode; commit 5 for contradiction-detector; commit 6 reranker.
	hIngest        prometheus.Histogram
	hRetrieval     prometheus.Histogram
	hContradiction prometheus.Histogram
	hRerank        prometheus.Histogram
}

// New constructs the Metrics struct with both legacy atomic counters and
// Prometheus collectors wired. Returned pointer is safe for concurrent use.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		promReg: reg,
		pStore: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_store_total",
			Help: "Total /store HTTP calls counted (atomic + prometheus in lockstep).",
		}),
		pSearch: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_search_total",
			Help: "Total /search HTTP calls counted.",
		}),
		pRetrieve: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_retrieve_total",
			Help: "Total /retrieve HTTP calls counted.",
		}),
		pIngest: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_ingest_total",
			Help: "Total ingestion events counted.",
		}),
		pQuery: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_query_total",
			Help: "Total /query HTTP calls counted.",
		}),
		pEdge: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_edge_total",
			Help: "Total edge operations counted.",
		}),
		pErr: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_errors_total",
			Help: "Total error responses counted across HTTP handlers.",
		}),
		pSchemaConflict: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_schema_conflict_total",
			Help: "Total 409 schema_conflict responses counted (SIGHUP raced mid-request).",
		}),
		pTaskStatus: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_task_status_total",
			Help: "Total /task/status calls counted.",
		}),
		pTaskExec: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_task_exec_total",
			Help: "Total /task/executable (and /task/next) calls counted.",
		}),
		pTaskList: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_task_list_total",
			Help: "Total /task/list calls counted.",
		}),
		pTaskShow: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_task_show_total",
			Help: "Total /task/show calls counted.",
		}),
		pTaskDep: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_task_dep_total",
			Help: "Total /task/dep calls counted.",
		}),
		pTaskRollback: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_task_rollback_total",
			Help: "Total /task/rollback calls counted.",
		}),
		pTaskTree: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_task_tree_total",
			Help: "Total /task/tree calls counted.",
		}),
		pTaskCreate: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_task_create_total",
			Help: "Total /task/create calls counted.",
		}),
		pRetentionRun: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_retention_run_total",
			Help: "Total retention GarbageCollector cycle runs counted.",
		}),

		// duration histograms (commit 2/8) — no labels yet.
		hIngest: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "hermem_ingest_duration_seconds",
			Help:    "End-to-end ingestion latency (request → store-complete). Bimodal: sub-100ms dedup-skip; 2-60s LLM extract path.",
			Buckets: durationBuckets,
		}),
		hRetrieval: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "hermem_retrieval_duration_seconds",
			Help:    "End-to-end retrieval/search latency (request → response). Includes embed + cosine + rerank overhead.",
			Buckets: durationBuckets,
		}),
		hContradiction: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "hermem_contradiction_duration_seconds",
			Help:    "Contradiction detection latency per scan (cosine pair-check + threshold walk).",
			Buckets: durationBuckets,
		}),
		hRerank: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "hermem_rerank_duration_seconds",
			Help:    "Reranker latency per candidate batch (cross-encoder or LLM-based; bound by 60s LLM timeout).",
			Buckets: durationBuckets,
		}),
	}
	reg.MustRegister(
		m.pStore, m.pSearch, m.pRetrieve, m.pIngest, m.pQuery, m.pEdge, m.pErr,
		m.pSchemaConflict,
		m.pTaskStatus, m.pTaskExec, m.pTaskList, m.pTaskShow, m.pTaskDep,
		m.pTaskRollback, m.pTaskTree, m.pTaskCreate,
		m.pRetentionRun,
	)
	reg.MustRegister(
		m.hIngest, m.hRetrieval, m.hContradiction, m.hRerank,
	)
	return m
}

// PrometheusRegistry returns the hermem-owned *prometheus.Registry. Used
// by commits 3-6 of the OBSERVABILITY sprint to register per-domain
// HistogramVec / GaugeVec / CounterVec collectors. Commits 7-8 wire the
// /metrics endpoint through promhttp.HandlerFor in src/internal/server/.
// Server handlers can pre-register per-domain collectors against this
// registry; New() already registers the 17 IncXxx counters + 4 duration
// histograms above.
func (m *Metrics) PrometheusRegistry() *prometheus.Registry { return m.promReg }

// PrometheusHandler returns the http.Handler that serves the hermem-owned
// registry via client_golang's promhttp driver (text exposition v0.0.4).
func (m *Metrics) PrometheusHandler() http.Handler {
	return promhttp.HandlerFor(m.promReg, promhttp.HandlerOpts{})
}

// IncXxx methods below bump BOTH the atomic counter (legacy view) AND
// the prometheus.Counter so /metrics reflects the same call counts that
// the legacy expvar-style counters did. Each pair is byte-compatible
// with pre-OBSERVABILITY callers.

func (m *Metrics) IncStore()          { m.storeCount.Add(1); m.pStore.Inc() }
func (m *Metrics) IncSearch()         { m.searchCount.Add(1); m.pSearch.Inc() }
func (m *Metrics) IncRetrieve()       { m.retrieveCount.Add(1); m.pRetrieve.Inc() }
func (m *Metrics) IncIngest()         { m.ingestCount.Add(1); m.pIngest.Inc() }
func (m *Metrics) IncQuery()          { m.queryCount.Add(1); m.pQuery.Inc() }
func (m *Metrics) IncEdge()           { m.edgeCount.Add(1); m.pEdge.Inc() }
func (m *Metrics) IncErr()            { m.errCount.Add(1); m.pErr.Inc() }
func (m *Metrics) IncSchemaConflict() { m.schemaConflictCount.Add(1); m.pSchemaConflict.Inc() }
func (m *Metrics) IncTaskStatus()     { m.taskStatusCount.Add(1); m.pTaskStatus.Inc() }
func (m *Metrics) IncTaskExec()       { m.taskExecCount.Add(1); m.pTaskExec.Inc() }
func (m *Metrics) IncTaskList()       { m.taskListCount.Add(1); m.pTaskList.Inc() }
func (m *Metrics) IncTaskShow()       { m.taskShowCount.Add(1); m.pTaskShow.Inc() }
func (m *Metrics) IncTaskDep()        { m.taskDepCount.Add(1); m.pTaskDep.Inc() }
func (m *Metrics) IncTaskRollback()   { m.taskRollbackCount.Add(1); m.pTaskRollback.Inc() }
func (m *Metrics) IncTaskTree()       { m.taskTreeCount.Add(1); m.pTaskTree.Inc() }
func (m *Metrics) IncTaskCreate()     { m.taskCreateCount.Add(1); m.pTaskCreate.Inc() }
func (m *Metrics) IncRetentionRun()   { m.retentionRunCount.Add(1); m.pRetentionRun.Inc() }

// ObserveXxx methods below record into the 4 commmit-2 duration histograms.
// Histograms are prometheus-native only (no atomic dual-track) — the legacy
// expvar-style handler is preserved verbatim above for the 17 counters.

// ObserveIngestDuration records end-to-end ingestion latency in seconds.
// Call once per /store invocation, after the response is composed.
func (m *Metrics) ObserveIngestDuration(seconds float64) { m.hIngest.Observe(seconds) }

// ObserveRetrievalDuration records end-to-end retrieval latency in seconds.
// Call once per /search, /retrieve, /query, /response, /provenance request.
func (m *Metrics) ObserveRetrievalDuration(seconds float64) { m.hRetrieval.Observe(seconds) }

// ObserveContradictionDuration records contradiction-detection latency per scan
// cycle. May exceed 60s on large graphs; samples cap at +Inf bucket.
func (m *Metrics) ObserveContradictionDuration(seconds float64) { m.hContradiction.Observe(seconds) }

// ObserveRerankDuration records reranker latency per batch invocation.
// Cross-encoder rerank is sub-100ms; LLM-based rerank is 2-60s by timeout.
func (m *Metrics) ObserveRerankDuration(seconds float64) { m.hRerank.Observe(seconds) }

// WriteExposition writes the legacy expvar-style Prometheus text-format
// dump of all 17 atomic counters. Preserved from e2aa722 verbatim so any
// /metrics-style endpoint that already calls this method keeps working.
// NOTE: histograms added in commit 2 are NOT emitted here — bucket counts
// require a multi-line Prometheus exposition format that the legacy
// expvar-style text isn't positioned to reproduce. Use PrometheusHandler()
// for the v0.0.4 text-format with histograms.
func (m *Metrics) WriteExposition(w io.Writer) error {
	_, err := fmt.Fprintf(w, "# HELP hermem_store_total Total store operations\n# TYPE hermem_store_total counter\nhermem_store_total %d\n", m.storeCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_search_total Total search operations\n# TYPE hermem_search_total counter\nhermem_search_total %d\n", m.searchCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_retrieve_total Total retrieve operations\n# TYPE hermem_retrieve_total counter\nhermem_retrieve_total %d\n", m.retrieveCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_ingest_total Total ingest operations\n# TYPE hermem_ingest_total counter\nhermem_ingest_total %d\n", m.ingestCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_query_total Total query operations\n# TYPE hermem_query_total counter\nhermem_query_total %d\n", m.queryCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_edge_total Total edge operations\n# TYPE hermem_edge_total counter\nhermem_edge_total %d\n", m.edgeCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_errors_total Total error responses\n# TYPE hermem_errors_total counter\nhermem_errors_total %d\n", m.errCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_schema_conflict_total Total 409 schema_conflict responses\n# TYPE hermem_schema_conflict_total counter\nhermem_schema_conflict_total %d\n", m.schemaConflictCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_task_status_total Total /task/status operations\n# TYPE hermem_task_status_total counter\nhermem_task_status_total %d\n", m.taskStatusCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_task_exec_total Total /task/executable operations\n# TYPE hermem_task_exec_total counter\nhermem_task_exec_total %d\n", m.taskExecCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_task_list_total Total /task/list operations\n# TYPE hermem_task_list_total counter\nhermem_task_list_total %d\n", m.taskListCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_task_show_total Total /task/show operations\n# TYPE hermem_task_show_total counter\nhermem_task_show_total %d\n", m.taskShowCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_task_dep_total Total /task/dep operations\n# TYPE hermem_task_dep_total counter\nhermem_task_dep_total %d\n", m.taskDepCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_task_rollback_total Total /task/rollback operations\n# TYPE hermem_task_rollback_total counter\nhermem_task_rollback_total %d\n", m.taskRollbackCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_task_tree_total Total /task/tree operations\n# TYPE hermem_task_tree_total counter\nhermem_task_tree_total %d\n", m.taskTreeCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_task_create_total Total /task/create operations\n# TYPE hermem_task_create_total counter\nhermem_task_create_total %d\n", m.taskCreateCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_retention_run_total Total retention GarbageCollector runs\n# TYPE hermem_retention_run_total counter\nhermem_retention_run_total %d\n", m.retentionRunCount.Load())
	return err
}

// MetricsHandler returns the legacy expvar-style http.Handler that
// emits Prometheus text-format from the atomic counters.
// Preserved from e2aa722 verbatim so any /metrics-style endpoint that
// already wires this method keeps working alongside the new client_golang
// registry exposed via PrometheusHandler().
func (m *Metrics) MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_ = m.WriteExposition(w)
	})
}
