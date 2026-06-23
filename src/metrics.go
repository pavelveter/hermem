package main

import (
	"expvar"
	"net/http"
)

var (
	metricStores         = expvar.NewInt("hermem_stores_total")
	metricSearches       = expvar.NewInt("hermem_searches_total")
	metricRetrieves      = expvar.NewInt("hermem_retrieves_total")
	metricIngests        = expvar.NewInt("hermem_ingests_total")
	metricQueries        = expvar.NewInt("hermem_queries_total")
	metricEdges          = expvar.NewInt("hermem_edges_total")
	metricErrs           = expvar.NewInt("hermem_errors_total")
	metricTaskStatus     = expvar.NewInt("hermem_task_status_total")
	metricTaskExecutable = expvar.NewInt("hermem_task_executable_total")
	metricTaskList       = expvar.NewInt("hermem_task_list_total")
	metricTaskShow       = expvar.NewInt("hermem_task_show_total")
	metricTaskDep        = expvar.NewInt("hermem_task_dep_total")
	metricTaskRollback   = expvar.NewInt("hermem_task_rollback_total")
	metricTaskNext       = expvar.NewInt("hermem_task_next_total")
	metricTaskCreate     = expvar.NewInt("hermem_task_create_total")
	metricTaskTree       = expvar.NewInt("hermem_task_tree_total")
)

func incStore()          { metricStores.Add(1) }
func incSearch()         { metricSearches.Add(1) }
func incRetrieve()       { metricRetrieves.Add(1) }
func incIngest()         { metricIngests.Add(1) }
func incQuery()          { metricQueries.Add(1) }
func incEdge()           { metricEdges.Add(1) }
func incErr()            { metricErrs.Add(1) }
func incTaskStatus()     { metricTaskStatus.Add(1) }
func incTaskExecutable() { metricTaskExecutable.Add(1) }
func incTaskList()       { metricTaskList.Add(1) }
func incTaskShow()       { metricTaskShow.Add(1) }
func incTaskDep()        { metricTaskDep.Add(1) }
func incTaskRollback()   { metricTaskRollback.Add(1) }
func incTaskNext()       { metricTaskNext.Add(1) }
func incTaskCreate()     { metricTaskCreate.Add(1) }
func incTaskTree()       { metricTaskTree.Add(1) }

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	expvar.Handler().ServeHTTP(w, r)
}
