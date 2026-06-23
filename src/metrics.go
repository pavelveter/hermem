package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

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
