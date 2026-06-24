package metrics

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestMetrics_ConcurrentIncrementStrictSum — many goroutines bump IncStore
// + IncErr in tight loops; the final load must equal exact attempt count.
// Race detector will fire if any counter is incremented outside atomic
// (atomic.Int64.Add is the only path through Inc*).
func TestMetrics_ConcurrentIncrementStrictSum(t *testing.T) {
	const goroutines = 32
	const perGoroutine = 1000
	m := New()

	var wg sync.WaitGroup
	wg.Add(2 * goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				m.IncStore()
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				m.IncErr()
			}
		}()
	}
	wg.Wait()

	want := int64(goroutines * perGoroutine)
	if got := m.storeCount.Load(); got != want {
		t.Fatalf("storeCount: want %d, got %d", want, got)
	}
	if got := m.errorCount.Load(); got != want {
		t.Fatalf("errorCount: want %d, got %d", want, got)
	}
}

// TestMetrics_AllIncHelpers_BumpDistinctCounters — every Inc* method touches
// a distinct atomic counter; otherwise one would silently steal another
// handler's instrumentation. Snapshot zero baseline, call every Inc* once,
// verify each counter advances by exactly 1.
func TestMetrics_AllIncHelpers_BumpDistinctCounters(t *testing.T) {
	m := New()
	// Each entry pairs the counter's private field name (the source-of-truth
	// atomic.Int64) with the inc method that should bump it. Reading the
	// private fields is allowed inside the same package, and the field-name
	// table is the canonical definition of "each counter is distinct".
	type pair struct {
		field func() int64
		call  func()
	}
	cases := []pair{
		{field: func() int64 { return m.storeCount.Load() }, call: m.IncStore},
		{field: func() int64 { return m.searchCount.Load() }, call: m.IncSearch},
		{field: func() int64 { return m.retrieveCount.Load() }, call: m.IncRetrieve},
		{field: func() int64 { return m.ingestCount.Load() }, call: m.IncIngest},
		{field: func() int64 { return m.queryCount.Load() }, call: m.IncQuery},
		{field: func() int64 { return m.edgeCount.Load() }, call: m.IncEdge},
		{field: func() int64 { return m.errorCount.Load() }, call: m.IncErr},
		{field: func() int64 { return m.schemaConflictCount.Load() }, call: m.IncSchemaConflict},
		{field: func() int64 { return m.taskStatusCount.Load() }, call: m.IncTaskStatus},
		{field: func() int64 { return m.taskExecCount.Load() }, call: m.IncTaskExec},
		{field: func() int64 { return m.taskListCount.Load() }, call: m.IncTaskList},
		{field: func() int64 { return m.taskShowCount.Load() }, call: m.IncTaskShow},
		{field: func() int64 { return m.taskDepCount.Load() }, call: m.IncTaskDep},
		{field: func() int64 { return m.taskRollbackCount.Load() }, call: m.IncTaskRollback},
		{field: func() int64 { return m.taskTreeCount.Load() }, call: m.IncTaskTree},
		{field: func() int64 { return m.taskCreateCount.Load() }, call: m.IncTaskCreate},
	}
	for _, p := range cases {
		p.call()
	}
	for i, p := range cases {
		if got := p.field(); got != 1 {
			t.Fatalf("case %d: want delta=1, got %d", i, got)
		}
	}
}

// TestMetrics_WriteExposition_PrometheusFormat — output must contain the
// canonical Prometheus exposition layout for every counter: `# HELP`,
// `# TYPE`, `<name> <value>`. A scraper depends on this byte format.
func TestMetrics_WriteExposition_PrometheusFormat(t *testing.T) {
	m := New()
	m.IncStore() // ensure at least one record gets written
	var sb strings.Builder
	m.WriteExposition(&sb)
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

// TestMetrics_Handler_HTTPContentTypeAndBody — Prometheus scraping requires
// `text/plain; version=0.0.4`. A regression here breaks Prometheus.
func TestMetrics_Handler_HTTPContentTypeAndBody(t *testing.T) {
	m := New()
	rr := httptest.NewRecorder()
	m.MetricsHandler(rr, httptest.NewRequest("GET", "/metrics", nil))
	if ct := rr.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4" {
		t.Fatalf("Content-Type: want text/plain; version=0.0.4, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), "hermem_store_total") {
		t.Fatalf("body: want exposition fragment, got %q", rr.Body.String())
	}
}

// TestMetrics_Isolated — two independent *Metrics instances must NOT share
// counter state. This is the regression guard for the package-global
// refactor: a future revert to package-level counters would surface as a
// failure here (m2.storeCount would equal m1.storeCount after m1.IncStore).
func TestMetrics_Isolated(t *testing.T) {
	m1 := New()
	m2 := New()
	m1.IncStore()
	m1.IncStore()
	m1.IncSearch()
	if got := m2.storeCount.Load(); got != 0 {
		t.Fatalf("m2.storeCount should be 0 (isolation broken), got %d", got)
	}
	if got := m2.searchCount.Load(); got != 0 {
		t.Fatalf("m2.searchCount should be 0 (isolation broken), got %d", got)
	}
	if got := m1.storeCount.Load(); got != 2 {
		t.Fatalf("m1.storeCount should be 2, got %d", got)
	}
	if got := m1.searchCount.Load(); got != 1 {
		t.Fatalf("m1.searchCount should be 1, got %d", got)
	}
}
