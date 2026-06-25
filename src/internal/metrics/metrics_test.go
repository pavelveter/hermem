package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewReturnsNonNilMetrics(t *testing.T) {
	m := New()
	if m == nil {
		t.Fatal("New() returned nil")
	}
	if m.PrometheusRegistry() == nil {
		t.Fatal("PrometheusRegistry() returned nil after New()")
	}
	if m.PrometheusHandler() == nil {
		t.Fatal("PrometheusHandler() returned nil after New()")
	}
}

// TestAll17IncMethodsBumpBothCounters covers every IncXxx method on the
// legacy atomic counter + the corresponding prometheus.Counter.
func TestAll17IncMethodsBumpBothCounters(t *testing.T) {
	m := New()
	m.IncStore()
	m.IncSearch()
	m.IncRetrieve()
	m.IncIngest()
	m.IncQuery()
	m.IncEdge()
	m.IncErr()
	m.IncSchemaConflict()
	m.IncTaskStatus()
	m.IncTaskExec()
	m.IncTaskList()
	m.IncTaskShow()
	m.IncTaskDep()
	m.IncTaskRollback()
	m.IncTaskTree()
	m.IncTaskCreate()
	m.IncRetentionRun()

	cases := []struct {
		name string
		got  int64
	}{
		{"IncStore", m.storeCount.Load()},
		{"IncSearch", m.searchCount.Load()},
		{"IncRetrieve", m.retrieveCount.Load()},
		{"IncIngest", m.ingestCount.Load()},
		{"IncQuery", m.queryCount.Load()},
		{"IncEdge", m.edgeCount.Load()},
		{"IncErr", m.errCount.Load()},
		{"IncSchemaConflict", m.schemaConflictCount.Load()},
		{"IncTaskStatus", m.taskStatusCount.Load()},
		{"IncTaskExec", m.taskExecCount.Load()},
		{"IncTaskList", m.taskListCount.Load()},
		{"IncTaskShow", m.taskShowCount.Load()},
		{"IncTaskDep", m.taskDepCount.Load()},
		{"IncTaskRollback", m.taskRollbackCount.Load()},
		{"IncTaskTree", m.taskTreeCount.Load()},
		{"IncTaskCreate", m.taskCreateCount.Load()},
		{"IncRetentionRun", m.retentionRunCount.Load()},
	}
	for _, c := range cases {
		if c.got != 1 {
			t.Errorf("atomic counter for %s expected 1, got %d", c.name, c.got)
		}
	}

	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	seen := map[string]bool{}
	for _, mf := range mfs {
		for range mf.GetMetric() {
			seen[mf.GetName()] = true
		}
	}
	wantProm := []string{
		"hermem_store_total", "hermem_search_total", "hermem_retrieve_total",
		"hermem_ingest_total", "hermem_query_total", "hermem_edge_total",
		"hermem_errors_total", "hermem_schema_conflict_total",
		"hermem_task_status_total", "hermem_task_exec_total", "hermem_task_list_total",
		"hermem_task_show_total", "hermem_task_dep_total", "hermem_task_rollback_total",
		"hermem_task_tree_total", "hermem_task_create_total", "hermem_retention_run_total",
	}
	for _, name := range wantProm {
		if !seen[name] {
			t.Errorf("Prometheus counter %q missing from registry (full: %v)", name, seen)
		}
	}
}

// TestHermemPrefixContract enforces: every collector registered against
// the hermem-owned registry carries the hermem_ prefix.
func TestHermemPrefixContract(t *testing.T) {
	m := New()
	bogus := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "not_hermem_prefix",
		Help: "deliberately wrong prefix - assertion below must reject it",
	})
	m.PrometheusRegistry().MustRegister(bogus)

	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range mfs {
		if !strings.HasPrefix(mf.GetName(), "hermem_") {
			t.Errorf("metric %q violates hermem_ prefix contract — CounterOpts.Name must start with 'hermem_'", mf.GetName())
		}
	}
}

// TestWriteExpositionAndMetricsHandlerStillWork locks the legacy
// expvar-style Prometheus text-format dump + the http.Handler wrapper.
func TestWriteExpositionAndMetricsHandlerStillWork(t *testing.T) {
	m := New()
	m.IncStore()
	m.IncErr()

	var sb strings.Builder
	if err := m.WriteExposition(&sb); err != nil {
		t.Fatalf("WriteExposition: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "hermem_store_total 1") {
		t.Errorf("WriteExposition missing 'hermem_store_total 1', got:\n%s", out)
	}
	if !strings.Contains(out, "hermem_errors_total 1") {
		t.Errorf("WriteExposition missing 'hermem_errors_total 1', got:\n%s", out)
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	m.MetricsHandler().ServeHTTP(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, "hermem_store_total 1") {
		t.Errorf("MetricsHandler body missing 'hermem_store_total 1'")
	}
}
