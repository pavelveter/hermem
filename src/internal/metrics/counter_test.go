package metrics

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestCounters_ConcurrentIncrementStrictSum — many goroutines bump IncStore
// + IncErr in tight loops; the final load must equal exact attempt count.
// Race detector will fire if any counter is incremented outside atomic.
// This is the contract that makes Prometheus exposition safe to scrape
// from any handler at any point.
func TestCounters_ConcurrentIncrementStrictSum(t *testing.T) {
	// Reset test isolates: snapshot each counter, run increments, then
	// check delta. Avoids cross-test pollution of package globals.
	const goroutines = 32
	const perGoroutine = 1000

	beforeStore := storeCount.Load()
	beforeErr := errorCount.Load()

	var wg sync.WaitGroup
	wg.Add(2 * goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				IncStore()
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				IncErr()
			}
		}()
	}
	wg.Wait()

	want := int64(goroutines * perGoroutine)
	if got := storeCount.Load() - beforeStore; got != want {
		t.Fatalf("store delta: want %d, got %d", want, got)
	}
	if got := errorCount.Load() - beforeErr; got != want {
		t.Fatalf("errors delta: want %d, got %d", want, got)
	}
}

// TestAllIncHelpers_BumpDistinctCounters — every exported Inc* helper
// must touch a distinct counter, otherwise one would silently steal
// another handler's instrumentation. We snapshot every counter, call
// every Inc* once, then verify each delta is exactly 1.
func TestAllIncHelpers_BumpDistinctCounters(t *testing.T) {
	snapshot := map[string]int64{
		"store":           storeCount.Load(),
		"search":          searchCount.Load(),
		"retrieve":        retrieveCount.Load(),
		"ingest":          ingestCount.Load(),
		"query":           queryCount.Load(),
		"edge":            edgeCount.Load(),
		"err":             errorCount.Load(),
		"schema_conflict": schemaConflictCount.Load(),
		"task_status":     taskStatusCount.Load(),
		"task_exec":       taskExecCount.Load(),
		"task_list":       taskListCount.Load(),
		"task_show":       taskShowCount.Load(),
		"task_dep":        taskDepCount.Load(),
		"task_rollback":   taskRollbackCnt.Load(),
		"task_tree":       taskTreeCount.Load(),
		"task_create":     taskCreateCnt.Load(),
	}
	IncStore()
	IncSearch()
	IncRetrieve()
	IncIngest()
	IncQuery()
	IncEdge()
	IncErr()
	IncSchemaConflict()
	IncTaskStatus()
	IncTaskExec()
	IncTaskList()
	IncTaskShow()
	IncTaskDep()
	IncTaskRollback()
	IncTaskTree()
	IncTaskCreate()

	for name, before := range snapshot {
		var after int64
		switch name {
		case "store":
			after = storeCount.Load()
		case "search":
			after = searchCount.Load()
		case "retrieve":
			after = retrieveCount.Load()
		case "ingest":
			after = ingestCount.Load()
		case "query":
			after = queryCount.Load()
		case "edge":
			after = edgeCount.Load()
		case "err":
			after = errorCount.Load()
		case "schema_conflict":
			after = schemaConflictCount.Load()
		case "task_status":
			after = taskStatusCount.Load()
		case "task_exec":
			after = taskExecCount.Load()
		case "task_list":
			after = taskListCount.Load()
		case "task_show":
			after = taskShowCount.Load()
		case "task_dep":
			after = taskDepCount.Load()
		case "task_rollback":
			after = taskRollbackCnt.Load()
		case "task_tree":
			after = taskTreeCount.Load()
		case "task_create":
			after = taskCreateCnt.Load()
		}
		if after-before != 1 {
			t.Fatalf("Inc for %s: want delta=1, got %d", name, after-before)
		}
	}
}

// TestWriteExposition_PrometheusFormat — output must contain the canonical
// Prometheus exposition layout for every counter: `# HELP`, `# TYPE`,
// `<name> <value>`. A scraper depends on this byte format.
func TestWriteExposition_PrometheusFormat(t *testing.T) {
	IncStore() // ensure at least one record gets written
	var sb strings.Builder
	WriteExposition(&sb)
	body := sb.String()
	for _, want := range []string{
		"# HELP hermem_store_total",
		"# TYPE hermem_store_total counter",
		"hermem_store_total ",
		"hermem_search_total ",
		"hermem_retrieve_total ",
		"hermem_ingest_total ",
		"hermem_query_total ",
		"hermem_edge_total ",
		"hermem_errors_total ",
		"hermem_schema_conflict_total ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("exposition missing %q\nfull body:\n%s", want, body)
		}
	}
}

// TestMetricsHandler_HTTPContentTypeAndBody — Prometheus scraping requires
// `text/plain; version=0.0.4`. A regression here breaks Prometheus.
func TestMetricsHandler_HTTPContentTypeAndBody(t *testing.T) {
	rr := httptest.NewRecorder()
	rr.Body.Reset() // httptest.NewRecorder may carry package state
	var sb strings.Builder
	WriteExposition(&sb)
	MetricsHandler(rr, httptest.NewRequest("GET", "/metrics", nil))
	if ct := rr.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4" {
		t.Fatalf("Content-Type: want text/plain; version=0.0.4, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), "hermem_store_total") {
		t.Fatalf("body: want exposition fragment, got %q", rr.Body.String())
	}
}
