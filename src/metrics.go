package main

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metricsDB is set by InitMetricsDB once the database handle is available.
// Used by the entity count gauge to provide live counts on each scrape.
var metricsDB *sql.DB

// InitMetricsDB wires the database handle into the metrics subsystem.
// Must be called after InitDB before serving metrics.
func InitMetricsDB(db *sql.DB) {
	metricsDB = db
}

var (
	metricStores = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_stores_total",
		Help: "Total number of store operations.",
	})
	metricSearches = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_searches_total",
		Help: "Total number of search operations.",
	})
	metricRetrieves = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_retrieves_total",
		Help: "Total number of retrieve operations.",
	})
	metricIngests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_ingests_total",
		Help: "Total number of ingest operations.",
	})
	metricQueries = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_queries_total",
		Help: "Total number of query operations.",
	})
	metricEdges = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_edges_total",
		Help: "Total number of edge operations.",
	})
	metricErrs = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_errors_total",
		Help: "Total number of error responses.",
	})
	metricTaskStatus = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_task_status_total",
		Help: "Total number of task status updates.",
	})
	metricTaskExecutable = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_task_executable_total",
		Help: "Total number of task executable queries.",
	})
	metricTaskList = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_task_list_total",
		Help: "Total number of task list queries.",
	})
	metricTaskShow = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_task_show_total",
		Help: "Total number of task show queries.",
	})
	metricTaskDep = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_task_dep_total",
		Help: "Total number of task dependency operations.",
	})
	metricTaskRollback = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_task_rollback_total",
		Help: "Total number of task rollback queries.",
	})
	metricTaskNext = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_task_next_total",
		Help: "Total number of task next queries (alias for executable).",
	})
	metricTaskCreate = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_task_create_total",
		Help: "Total number of task create operations.",
	})
	metricTaskTree = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hermem_task_tree_total",
		Help: "Total number of task tree queries.",
	})

	metricEntityCount = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "hermem_entities_count",
		Help: "Current number of active (non-archived) entities in the database.",
	}, func() float64 {
		if metricsDB == nil {
			return 0
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var count int64
		if err := metricsDB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM entities WHERE archived = 0`,
		).Scan(&count); err != nil {
			return 0
		}
		return float64(count)
	})
)

func init() {
	prometheus.MustRegister(
		metricStores,
		metricSearches,
		metricRetrieves,
		metricIngests,
		metricQueries,
		metricEdges,
		metricErrs,
		metricTaskStatus,
		metricTaskExecutable,
		metricTaskList,
		metricTaskShow,
		metricTaskDep,
		metricTaskRollback,
		metricTaskNext,
		metricTaskCreate,
		metricTaskTree,
		metricEntityCount,
	)
}

func incStore()          { metricStores.Inc() }
func incSearch()         { metricSearches.Inc() }
func incRetrieve()       { metricRetrieves.Inc() }
func incIngest()         { metricIngests.Inc() }
func incQuery()          { metricQueries.Inc() }
func incEdge()           { metricEdges.Inc() }
func incErr()            { metricErrs.Inc() }
func incTaskStatus()     { metricTaskStatus.Inc() }
func incTaskExecutable() { metricTaskExecutable.Inc() }
func incTaskList()       { metricTaskList.Inc() }
func incTaskShow()       { metricTaskShow.Inc() }
func incTaskDep()        { metricTaskDep.Inc() }
func incTaskRollback()   { metricTaskRollback.Inc() }
func incTaskNext()       { metricTaskNext.Inc() }
func incTaskCreate()     { metricTaskCreate.Inc() }
func incTaskTree()       { metricTaskTree.Inc() }

var promHandler = promhttp.Handler()

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	promHandler.ServeHTTP(w, r)
}
