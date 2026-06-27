// Package metrics owns hermem_* Prometheus metrics and exposes a single
// hermem-owned *prometheus.Registry the HTTP layer can serve via promhttp.
//
// All per-domain collectors MUST register on the SAME *prometheus.Registry
// returned by PrometheusRegistry(). Do NOT call prometheus.MustRegister /
// promauto — that pins to the global default.
//
// Cardinality discipline: each HistogramVec/CounterVec has a single label
// with a small, bounded value-set (<=10 expected).
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// durationBuckets is shared by the 4 commit-2/3 duration histograms.
// Sized for hermem's bimodal latency: fast embeddings (~50ms) vs slow LLM
// extraction (2-60s). Future commits may override at construction time if
// the production latency distribution shifts; do NOT mutate this slice
// ad-hoc whenever a new histogram domain appears.
var durationBuckets = []float64{0.05, 0.1, 0.5, 1, 2, 5, 10, 15, 30, 60}

// knownCategories is the bounded value-set for the hIngest `category` label.
// Adding a new value here is the only path to introduce a new ingest
// latency dimension — a future ingest-side change that adds a new category
// string MUST extend this slice AND bump the assertion in
// TestHermemPrefixContract_KnownCategorySet.
// Cardinality math: 4 values (including "_init" sentinel) × (11 buckets +
// _sum + _count) = 52 time-series per scrape. ~80 was an earlier upper
// bound; the tight figure is 52.
var knownCategories = []string{"_init", "observation", "world", "task", "edge"}

// knownModes is the bounded value-set for the hRetrieval `mode` label.
// Pre-warm sentinel "_init" + the 6 retrieval endpoints wired in src/internal/server/retrieval:
// "/search" / "/retrieve" / "/query" / "/response" / "/query/explain" / "/provenance".
// Math: 7 Ã (10 durationBuckets + +Inf auto-added = 11 buckets) + _sum + _count
//
//	= 7 Ã 13 = 91 time-series per scrape. Adding a new endpoint = extend this slice
//
// + bump TestHermemPrefixContract_KnownModesSet.
var knownModes = []string{"_init", "search", "retrieve", "query", "response", "query_explain", "provenance"}

// knownDetectors is the bounded value-set for the hContradiction `detector` label.
// Aligned with src/internal/contradiction's actual detectors (verified via
// code-searcher at C5 amend time): "lexical" (NewLexicalDetector in lexical.go),
// "composite" (NewCompositeDetector in composite.go). "semantic" is referenced
// in detector.go + lexical.go comments as a FUTURE detector (PHASE 2.4) but
// does not yet have a concrete implementation — excluded from the bounded
// value-set until it lands. When the semantic detector commits, extend this
// slice + bump TestHermemPrefixContract_KnownDetectorsSet.
// Math: 3 Ã (10 durationBuckets + +Inf = 11 buckets + _sum + _count)
//
//	= 3 Ã 13 = 39 time-series per scrape.
var knownDetectors = []string{"_init", "lexical", "composite"}

// knownStrategies is the bounded value-set for the hRerank `strategy` label.
// Three strategies today (per src/internal/ai/reranker.go verification):
//
//	"cross_encoder" — future cross-encoder rerank (not yet implemented)
//	"llm"          — LLM-based rerank (NewOpenAIReranker / NewOllamaReranker)
//	"cosine_only"  — NoopReranker / raw-cosine order, no actual rerank call
//
// Pre-warm sentinel "_init" sits at index 0 as in C3-C5. Math: 4 ×
// (10 durationBuckets + +Inf = 11 buckets + _sum + _count) = 4 × 13 = 52
// time-series per scrape. Add a new strategy = extend + bump TestKnownStrategiesSet.
var knownStrategies = []string{"_init", "llm_openai", "llm_ollama", "noop"}

// Metrics is the hermem-wide counter + histogram bag.
//
// Migration cheat-sheet (when adding a new IncXxx counter):
//
//	A maintainer adding e.g. IncConnect must edit 7 spots in this file:
//
//	1. Struct atomic field — `connectCount atomic.Int64` in atomic block.
//	2. Struct prom field — `pConnect prometheus.Counter` in prom block.
//	3. New() init — `m.pConnect = prometheus.NewCounter(CounterOpts{Name: "hermem_connect_total", Help: "..."})`.
//	4. New() MustRegister — append m.pConnect to the vararg list.
//	5. IncXxx method body — `m.connectCount.Add(1); m.pConnect.Inc()`.
//	6. WriteExposition line — `fmt.Fprintf(... "# TYPE ..." "hermem_connect_total %d\n", m.connectCount.Load())`.
//	7. metrics_test.go test cases + wantProm slice.
//
// Migration cheat-sheet (when upgrading a single Histogram to HistogramVec):
//
//	A maintainer upgrading e.g. hExport to labelled-by-stage must edit:
//
//	1. Struct field — change `hExport prometheus.Histogram` to `*prometheus.HistogramVec`.
//	2. New() init — switch NewHistogram to NewHistogramVec with []string{"stage"} label.
//	3. MustRegister — still `m.hExport` (Vec registers itself).
//	4. Observe method — old `m.hExport.Observe(seconds)` becomes
//	   `m.hExport.WithLabelValues(stage).Observe(seconds)` and a label
//	   parameter is added to the method signature.
//	5. Pre-warm in New() — CALL m.hExport.WithLabelValues(stage) after
//	   construction with the "_init" sentinel so the parent MetricFamily
//	   shows in cold-start Gather(). Without this, /metrics returns an
//	   empty histogram until the first caller observation.
//	6. Callers — every call-site of the old Observe must pass a label
//	   string. Use a stable sentinel from knownCategories (or its
//	   per-domain equivalent) for unlabeled flows.
//	7. metrics_test.go — every Observe call site in test fixtures must
//	   gain the label arg; aggregation patterns work unchanged.
type Metrics struct {
	// atomic.Int64 fields preserved from e2aa722 verbatim (16, not 17 —
	// see commit 1/8 for the full restore). Keep server-side callers
	// byte-compatible.
	storeCount            atomic.Int64
	searchCount           atomic.Int64
	retrieveCount         atomic.Int64
	ingestCount           atomic.Int64
	queryCount            atomic.Int64
	edgeCount             atomic.Int64
	errCount              atomic.Int64
	schemaConflictCount   atomic.Int64
	taskStatusCount       atomic.Int64
	taskExecCount         atomic.Int64
	taskListCount         atomic.Int64
	taskShowCount         atomic.Int64
	taskDepCount          atomic.Int64
	taskRollbackCount     atomic.Int64
	taskTreeCount         atomic.Int64
	taskCreateCount       atomic.Int64
	retentionRunCount     atomic.Int64
	graphComponentsCount  atomic.Int64
	graphCommunitiesCount atomic.Int64
	graphVerifyCount      atomic.Int64

	// Prometheus counters (OBSERVABILITY commit 1/8).
	promReg           *prometheus.Registry
	pStore            prometheus.Counter
	pSearch           prometheus.Counter
	pRetrieve         prometheus.Counter
	pIngest           prometheus.Counter
	pQuery            prometheus.Counter
	pEdge             prometheus.Counter
	pErr              prometheus.Counter
	pSchemaConflict   prometheus.Counter
	pTaskStatus       prometheus.Counter
	pTaskExec         prometheus.Counter
	pTaskList         prometheus.Counter
	pTaskShow         prometheus.Counter
	pTaskDep          prometheus.Counter
	pTaskRollback     prometheus.Counter
	pTaskTree         prometheus.Counter
	pTaskCreate       prometheus.Counter
	pRetentionRun     prometheus.Counter
	pGraphComponents  prometheus.Counter
	pGraphCommunities prometheus.Counter
	pGraphVerify      prometheus.Counter

	// Prometheus histograms (OBSERVABILITY commits 2-3/8). hIngest was
	// promoted to *HistogramVec at C3; hRetrieval / hContradiction / hRerank
	// remain as single Histograms until commits 4-6/8.
	//
	// hIngest cardinality: 4 known categories ("observation" / "world" /
	// "task" / "edge") + 1 system sentinel ("_init") = 5 values. Total
	// time-series = 5 * (11 buckets + _sum + _count) = 65 per scrape.
	// If a future ingest-side change adds a new category, add it to
	// knownCategories and bump the TestHermemPrefixContract_KnownCategorySet
	// assertion. DO NOT pass user-supplied data verbatim — Prometheus
	// fans out one time-series per (label-value, bucket, _sum/_count), so
	// 10k unique categories would explode scrape size + scrape memory.
	//
	// Dashboard authors: filter `category=~"^(observation|world|task|edge)$"`
	// (regex excluding "_init" sentinel) so the system-emitted zero-presence
	// child never gets confused with a real category.
	hIngest           *prometheus.HistogramVec
	hRetrieval        *prometheus.HistogramVec
	hContradiction    *prometheus.HistogramVec
	hRerank           *prometheus.HistogramVec
	hGraphCommunities prometheus.Histogram
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
		pGraphComponents: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_graph_components_total",
			Help: "Total /connected-components calls counted.",
		}),
		pGraphCommunities: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_graph_communities_total",
			Help: "Total /communities calls counted.",
		}),
		pGraphVerify: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hermem_graph_verify_total",
			Help: "Total /graph/verify calls counted.",
		}),

		// hIngest as *HistogramVec (commit 3/8). Single label `category`.
		hIngest: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hermem_ingest_duration_seconds",
			Help:    "End-to-end ingestion latency (request -> store-complete) labelled by entity category. Bimodal: sub-100ms dedup-skip; 2-60s LLM extract path.",
			Buckets: durationBuckets,
		}, []string{"category"}),

		// hRetrieval / hContradiction / hRerank remain as single Histograms.
		hRetrieval: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hermem_retrieval_duration_seconds",
			Help:    "End-to-end retrieval/search latency (request -> response) labelled by retrieval mode (one of the 6 read-side endpoints). Includes embed + cosine + rerank overhead.",
			Buckets: durationBuckets,
		}, []string{"mode"}),
		hContradiction: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hermem_contradiction_duration_seconds",
			Help:    "Contradiction detection latency per scan labelled by detector (one of the 4 contradiction algorithms).",
			Buckets: durationBuckets,
		}, []string{"detector"}),
		hRerank: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hermem_rerank_duration_seconds",
			Help:    "Reranker latency per candidate batch labelled by strategy (one of: cross_encoder / llm / cosine_only).",
			Buckets: durationBuckets,
		}, []string{"strategy"}),

		hGraphCommunities: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "hermem_graph_communities_duration_seconds",
			Help:    "Louvain community-detection latency. Long-tail — large graphs can take seconds.",
			Buckets: durationBuckets,
		}),
	}
	reg.MustRegister(
		m.pStore, m.pSearch, m.pRetrieve, m.pIngest, m.pQuery, m.pEdge, m.pErr,
		m.pSchemaConflict,
		m.pTaskStatus, m.pTaskExec, m.pTaskList, m.pTaskShow, m.pTaskDep,
		m.pTaskRollback, m.pTaskTree, m.pTaskCreate,
		m.pRetentionRun,
		m.pGraphComponents, m.pGraphCommunities, m.pGraphVerify,
	)
	reg.MustRegister(
		m.hIngest, m.hRetrieval, m.hContradiction, m.hRerank,
		m.hGraphCommunities,
	)
	// Pre-warm hIngest: HistogramVec only surfaces in Gather() once at least
	// one labelled child is materialized via WithLabelValues. The "_init"
	// sentinel keeps the parent MetricFamily visible on cold-start /metrics
	// scrapes (Grafana dashboards depend on the parent name being present).
	// Callers MUST NOT pass category="_init" — it is a system-only sentinel.
	m.hIngest.WithLabelValues(knownCategories[0])       // knownCategories[0] = "_init"
	m.hRetrieval.WithLabelValues(knownModes[0])         // knownModes[0] = "_init"
	m.hContradiction.WithLabelValues(knownDetectors[0]) // knownDetectors[0] = "_init"
	m.hRerank.WithLabelValues(knownStrategies[0])       // knownStrategies[0] = "_init"
	return m
}

// PrometheusRegistry returns the hermem-owned *prometheus.Registry. Used
// by commits 3-6 of the OBSERVABILITY sprint to register per-domain
// HistogramVec / GaugeVec / CounterVec collectors; commits 7-8 wire the
// /metrics endpoint through promhttp.HandlerFor in src/internal/server/.
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

func (m *Metrics) IncStore()            { m.storeCount.Add(1); m.pStore.Inc() }
func (m *Metrics) IncSearch()           { m.searchCount.Add(1); m.pSearch.Inc() }
func (m *Metrics) IncRetrieve()         { m.retrieveCount.Add(1); m.pRetrieve.Inc() }
func (m *Metrics) IncIngest()           { m.ingestCount.Add(1); m.pIngest.Inc() }
func (m *Metrics) IncQuery()            { m.queryCount.Add(1); m.pQuery.Inc() }
func (m *Metrics) IncEdge()             { m.edgeCount.Add(1); m.pEdge.Inc() }
func (m *Metrics) IncErr()              { m.errCount.Add(1); m.pErr.Inc() }
func (m *Metrics) IncSchemaConflict()   { m.schemaConflictCount.Add(1); m.pSchemaConflict.Inc() }
func (m *Metrics) IncTaskStatus()       { m.taskStatusCount.Add(1); m.pTaskStatus.Inc() }
func (m *Metrics) IncTaskExec()         { m.taskExecCount.Add(1); m.pTaskExec.Inc() }
func (m *Metrics) IncTaskList()         { m.taskListCount.Add(1); m.pTaskList.Inc() }
func (m *Metrics) IncTaskShow()         { m.taskShowCount.Add(1); m.pTaskShow.Inc() }
func (m *Metrics) IncTaskDep()          { m.taskDepCount.Add(1); m.pTaskDep.Inc() }
func (m *Metrics) IncTaskRollback()     { m.taskRollbackCount.Add(1); m.pTaskRollback.Inc() }
func (m *Metrics) IncTaskTree()         { m.taskTreeCount.Add(1); m.pTaskTree.Inc() }
func (m *Metrics) IncTaskCreate()       { m.taskCreateCount.Add(1); m.pTaskCreate.Inc() }
func (m *Metrics) IncRetentionRun()     { m.retentionRunCount.Add(1); m.pRetentionRun.Inc() }
func (m *Metrics) IncGraphComponents()  { m.graphComponentsCount.Add(1); m.pGraphComponents.Inc() }
func (m *Metrics) IncGraphCommunities() { m.graphCommunitiesCount.Add(1); m.pGraphCommunities.Inc() }
func (m *Metrics) IncGraphVerify()      { m.graphVerifyCount.Add(1); m.pGraphVerify.Inc() }

// ObserveIngestDuration records end-to-end ingestion latency in seconds,
// labelled by `category`. The label MUST be one of the values in
// knownCategories (observation / world / task / edge); passing the system
// sentinel "_init" is undefined behaviour (callers should never observe it —
// it's pre-warmed solely so cold-start /metrics has a non-empty parent MF).
// Pre-C3 callers break intentionally at this commit boundary.
//
// Dashboard authors: filter `category=~"^(observation|world|task|edge)$"`
// to exclude the system "_init" sentinel from panel queries.
func (m *Metrics) ObserveIngestDuration(seconds float64, category string) {
	m.hIngest.WithLabelValues(category).Observe(seconds)
}

// ObserveRetrievalDuration records end-to-end retrieval latency in seconds,
// labelled by `mode` (one of the 6 read-side endpoints: search / retrieve /
// query / response / query_explain / provenance). Caller MUST pass a value
// from knownModes; "_init" is reserved for the system's pre-warm child.
// Pre-C4 callers break intentionally at this commit boundary.
//
// Dashboard authors: filter mode=~"^(search|retrieve|query|response|query_explain|provenance)$"
// to exclude the system "_init" sentinel.
func (m *Metrics) ObserveRetrievalDuration(seconds float64, mode string) {
	m.hRetrieval.WithLabelValues(mode).Observe(seconds)
}

// ObserveContradictionDuration records contradiction-detection latency per
// scan, labelled by `detector` (one of: exact / embedding / temporal / rule).
// Caller MUST pass a value from knownDetectors; "_init" is reserved for the
// system's pre-warm child. Pre-C5 callers break intentionally at this
// commit boundary.
//
// Dashboard authors: filter detector=~"^(exact|embedding|temporal|rule)$"
// to exclude the system "_init" sentinel.
func (m *Metrics) ObserveContradictionDuration(seconds float64, detector string) {
	m.hContradiction.WithLabelValues(detector).Observe(seconds)
}

// ObserveRerankDuration records reranker latency per candidate batch,
// labelled by `strategy`. Caller MUST pass a value from knownStrategies;
// "_init" is reserved for the system's pre-warm child. Pre-C6 callers
// break intentionally at this commit boundary.
//
// Dashboard authors: filter strategy=~"^(cross_encoder|llm|cosine_only)$"
// to exclude the system "_init" sentinel.
func (m *Metrics) ObserveRerankDuration(seconds float64, strategy string) {
	m.hRerank.WithLabelValues(strategy).Observe(seconds)
}

// ObserveGraphCommunitiesDuration records Louvain community-detection
// latency in seconds.
func (m *Metrics) ObserveGraphCommunitiesDuration(seconds float64) {
	m.hGraphCommunities.Observe(seconds)
}

// WriteExposition writes the legacy expvar-style Prometheus text-format
// dump of all 17 atomic counters. Preserved verbatim from e2aa722 so any
// /metrics-style endpoint that already calls it keeps working.
// NOTE: histograms added in commit 2/8 are NOT emitted here — bucket counts
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
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_graph_components_total Total /connected-components calls\n# TYPE hermem_graph_components_total counter\nhermem_graph_components_total %d\n", m.graphComponentsCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_graph_communities_total Total /communities calls\n# TYPE hermem_graph_communities_total counter\nhermem_graph_communities_total %d\n", m.graphCommunitiesCount.Load())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "# HELP hermem_graph_verify_total Total /graph/verify calls\n# TYPE hermem_graph_verify_total counter\nhermem_graph_verify_total %d\n", m.graphVerifyCount.Load())
	return err
}

// Snapshot returns a JSON-friendly map of the 20 atomic counter
// values keyed by Prometheus metric name (e.g. "hermem_store_total").
// Used by the health service to surface a compact process-metrics
// view alongside the standard "status: ok" envelope without forcing
// /health consumers to also parse /metrics. Counters are read
// individually via atomic.Int64.Load(); the snapshot is a
// point-in-time view, not transactionally consistent across the
// 20 atomic loads — acceptable for monitoring, NOT for
// arithmetic across counters (e.g. summing store + search).
//
// Named to match the Prometheus v0.0.4 text format that
// WriteExposition emits so a consumer reading both surfaces sees
// the same key names.
func (m *Metrics) Snapshot() map[string]uint64 {
	return map[string]uint64{
		"hermem_store_total":             uint64(m.storeCount.Load()),
		"hermem_search_total":            uint64(m.searchCount.Load()),
		"hermem_retrieve_total":          uint64(m.retrieveCount.Load()),
		"hermem_ingest_total":            uint64(m.ingestCount.Load()),
		"hermem_query_total":             uint64(m.queryCount.Load()),
		"hermem_edge_total":              uint64(m.edgeCount.Load()),
		"hermem_errors_total":            uint64(m.errCount.Load()),
		"hermem_schema_conflict_total":   uint64(m.schemaConflictCount.Load()),
		"hermem_task_status_total":       uint64(m.taskStatusCount.Load()),
		"hermem_task_exec_total":         uint64(m.taskExecCount.Load()),
		"hermem_task_list_total":         uint64(m.taskListCount.Load()),
		"hermem_task_show_total":         uint64(m.taskShowCount.Load()),
		"hermem_task_dep_total":          uint64(m.taskDepCount.Load()),
		"hermem_task_rollback_total":     uint64(m.taskRollbackCount.Load()),
		"hermem_task_tree_total":         uint64(m.taskTreeCount.Load()),
		"hermem_task_create_total":       uint64(m.taskCreateCount.Load()),
		"hermem_retention_run_total":     uint64(m.retentionRunCount.Load()),
		"hermem_graph_components_total":  uint64(m.graphComponentsCount.Load()),
		"hermem_graph_communities_total": uint64(m.graphCommunitiesCount.Load()),
		"hermem_graph_verify_total":      uint64(m.graphVerifyCount.Load()),
	}
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
