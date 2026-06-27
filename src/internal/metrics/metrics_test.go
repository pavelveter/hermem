package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
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
	m.IncGraphComponents()
	m.IncGraphCommunities()
	m.IncGraphVerify()

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
		{"IncGraphComponents", m.graphComponentsCount.Load()},
		{"IncGraphCommunities", m.graphCommunitiesCount.Load()},
		{"IncGraphVerify", m.graphVerifyCount.Load()},
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
		"hermem_graph_components_total", "hermem_graph_communities_total",
		"hermem_graph_verify_total",
	}
	for _, name := range wantProm {
		if !seen[name] {
			t.Errorf("Prometheus counter %q missing from registry (full: %v)", name, seen)
		}
	}
}

// TestHermemPrefixContract: every metric registered against the hermem-owned
// registry carries the hermem_ prefix. Precondition for namespace trust.
// If a future contributor adds a non-hermem_ metric by accident, this test
// fires t.Errorf pointing at the offending metric.
func TestHermemPrefixContract(t *testing.T) {
	m := New()
	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(mfs) == 0 {
		t.Fatal("expected at least 1 metric family in registry, got 0")
	}
	for _, mf := range mfs {
		if !strings.HasPrefix(mf.GetName(), "hermem_") {
			t.Errorf("metric %q violates hermem_ prefix contract \u2014 CounterOpts/HistogramOpts.Name must start with 'hermem_'", mf.GetName())
		}
	}
}

// TestHermemPrefixContract_AllHermemMetricsPresent is the positive
// regression: all 17 IncXxx counters + 4 duration histograms (added in
// commit 2/8) are visible to Gather(). Pure-name check \u2014 type + Help are
// asserted by separate tests below so regressions report unambiguously.
func TestHermemPrefixContract_AllHermemMetricsPresent(t *testing.T) {
	m := New()
	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	want := []string{
		"hermem_store_total", "hermem_search_total", "hermem_retrieve_total",
		"hermem_ingest_total", "hermem_query_total", "hermem_edge_total",
		"hermem_errors_total", "hermem_schema_conflict_total",
		"hermem_task_status_total", "hermem_task_exec_total", "hermem_task_list_total",
		"hermem_task_show_total", "hermem_task_dep_total", "hermem_task_rollback_total",
		"hermem_task_tree_total", "hermem_task_create_total", "hermem_retention_run_total",
		"hermem_ingest_duration_seconds", "hermem_retrieval_duration_seconds",
		"hermem_contradiction_duration_seconds", "hermem_rerank_duration_seconds",
		"hermem_graph_communities_duration_seconds",
	}
	seen := map[string]bool{}
	for _, mf := range mfs {
		seen[mf.GetName()] = true
	}
	for _, name := range want {
		if !seen[name] {
			t.Errorf("expected %q in registry (got: %v)", name, seen)
		}
	}
}

// TestHermemPrefixContract_CorrectCollectorTypes asserts each hermem
// metric family carries the correct dto.MetricType. Counter for *_total
// (IncXxx pairs); HISTOGRAM for *_duration_seconds.
//
// NOTE: prometheus.ValueType is NOT used here because v1.21.0's enum only
// publishes CounterValue/GaugeValue/UntypedValue (histograms aren't scalar).
// dto.MetricType_* protobuf enum is the only stable comparison interface.
// Path A (prometheus.ValueType(mf.GetType())) was a non-viable alternative
// for the histogram kind specifically. See C2 commit message.
func TestHermemPrefixContract_CorrectCollectorTypes(t *testing.T) {
	m := New()
	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	want := map[string]dto.MetricType{
		"hermem_store_total":                        dto.MetricType_COUNTER,
		"hermem_search_total":                       dto.MetricType_COUNTER,
		"hermem_retrieve_total":                     dto.MetricType_COUNTER,
		"hermem_ingest_total":                       dto.MetricType_COUNTER,
		"hermem_query_total":                        dto.MetricType_COUNTER,
		"hermem_edge_total":                         dto.MetricType_COUNTER,
		"hermem_errors_total":                       dto.MetricType_COUNTER,
		"hermem_schema_conflict_total":              dto.MetricType_COUNTER,
		"hermem_task_status_total":                  dto.MetricType_COUNTER,
		"hermem_task_exec_total":                    dto.MetricType_COUNTER,
		"hermem_task_list_total":                    dto.MetricType_COUNTER,
		"hermem_task_show_total":                    dto.MetricType_COUNTER,
		"hermem_task_dep_total":                     dto.MetricType_COUNTER,
		"hermem_task_rollback_total":                dto.MetricType_COUNTER,
		"hermem_task_tree_total":                    dto.MetricType_COUNTER,
		"hermem_task_create_total":                  dto.MetricType_COUNTER,
		"hermem_retention_run_total":                dto.MetricType_COUNTER,
		"hermem_ingest_duration_seconds":            dto.MetricType_HISTOGRAM,
		"hermem_retrieval_duration_seconds":         dto.MetricType_HISTOGRAM,
		"hermem_contradiction_duration_seconds":     dto.MetricType_HISTOGRAM,
		"hermem_rerank_duration_seconds":            dto.MetricType_HISTOGRAM,
		"hermem_graph_components_total":             dto.MetricType_COUNTER,
		"hermem_graph_communities_total":            dto.MetricType_COUNTER,
		"hermem_graph_verify_total":                 dto.MetricType_COUNTER,
		"hermem_graph_communities_duration_seconds": dto.MetricType_HISTOGRAM,
	}
	for _, mf := range mfs {
		wantType, tracked := want[mf.GetName()]
		if !tracked {
			continue
		}
		if mf.GetType() != wantType {
			t.Errorf("%q type expected %v, got %v", mf.GetName(), wantType, mf.GetType())
		}
	}
}

// TestHermemPrefixContract_NonEmptyHelp asserts every hermem metric
// carries a non-empty Help string \u2014 Grafana legends render blank text
// when Help is empty, which is a noticeable UX regression.
func TestHermemPrefixContract_NonEmptyHelp(t *testing.T) {
	m := New()
	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetHelp() == "" {
			t.Errorf("%q has empty Help \u2014 CounterOpts/HistogramOpts.Help must remain non-empty so /metrics exposes meaningful docstrings (Grafana legends render blank text otherwise)", mf.GetName())
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

// TestDurationHistograms exercises the 4 commit-2 duration histograms.
//
// C3-stable: the assertions sum over all labelled children of the
// MetricFamily (mf.GetMetric()). Today each histogram has exactly one
// child (no labels yet), so the loop is degenerate. After C3 promotes
// hIngest to *HistogramVec with a category label, mf.GetMetric() returns
// N children; the sum-with-iteration pattern still yields the correct
// aggregate counts/sums without depending on label-string ordering. This
// test will continue to pass after C3 without modification.
func TestDurationHistograms(t *testing.T) {
	m := New()

	// Deliberately distinct values: 0.3s (embed-like), 5s (LLM-fast),
	// 25s (LLM-slow), 45s (LLM-extreme). All four span distinct buckets
	// in the .05 .1 .5 1 2 5 10 15 30 60 layout.
	m.ObserveIngestDuration(0.3, "observation")
	m.ObserveRetrievalDuration(5, "search")
	m.ObserveContradictionDuration(25, "lexical")
	m.ObserveRerankDuration(45, "llm_ollama")
	// Second observation on ingest to confirm count tracks multiple samples.
	m.ObserveIngestDuration(0.7, "observation")

	want := []struct {
		name      string
		wantCount uint64
		wantSum   float64
	}{
		{"hermem_ingest_duration_seconds", 2, 1.0}, // 0.3 + 0.7
		{"hermem_retrieval_duration_seconds", 1, 5.0},
		{"hermem_contradiction_duration_seconds", 1, 25.0},
		{"hermem_rerank_duration_seconds", 1, 45.0},
	}

	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, w := range want {
		mf := findMF(mfs, w.name)
		if mf == nil {
			t.Errorf("histogram %q missing from registry", w.name)
			continue
		}
		// Sum across all labelled children so the test is robust to
		// C3's *HistogramVec upgrade (children are conceptually the
		// per-label split of the same observation set).
		var totalCount uint64
		var totalSum float64
		for _, child := range mf.GetMetric() {
			totalCount += child.GetHistogram().GetSampleCount()
			totalSum += child.GetHistogram().GetSampleSum()
		}
		if totalCount != w.wantCount {
			t.Errorf("%s aggregate sample count expected %d, got %d", w.name, w.wantCount, totalCount)
		}
		diff := totalSum - w.wantSum
		if diff < 0 {
			diff = -diff
		}
		if diff > 1e-9 {
			t.Errorf("%s aggregate sample sum expected %v, got %v", w.name, w.wantSum, totalSum)
		}
	}
}

// TestFindMF_ReturnsNilOnMiss locks the helper's nil-return contract
// so TestDurationHistograms (and any future caller) doesn't silently
// observe a wrong MF on a name typo. Also pins the empty-slice case.
func TestFindMF_ReturnsNilOnMiss(t *testing.T) {
	if got := findMF(nil, "any"); got != nil {
		t.Errorf("nil mfs: expected nil, got %v", got)
	}
	if got := findMF([]*dto.MetricFamily{}, "any"); got != nil {
		t.Errorf("empty literal mfs: expected nil, got %v", got)
	}
	// make-built empty slice must also return nil on miss.
	if got := findMF(make([]*dto.MetricFamily, 0), "any"); got != nil {
		t.Errorf("make-built empty mfs: expected nil, got %v", got)
	}
}

// findMF returns a *dto.MetricFamily by name, or nil if not present.
// Centralises the linear scan so individual tests stay clean. Stable
// across HistogramVec upgraded children in C3 because the parent family
// (and its GetName) is unchanged.
func findMF(mfs []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

// TestHermemPrefixContract_KnownCategorySet enforces the bounded-value-set
// contract on the hIngest `category` label. Adding a new ingest-side
// category MUST extend knownCategories in metrics.go AND bump the
// `want` slice below. Total time-series math: len(knownCategories) *
// (11 buckets + _sum + _count) per scrape.
func TestHermemPrefixContract_KnownCategorySet(t *testing.T) {
	m := New()
	// Each known category must materialize a labelled child on demand.
	for _, cat := range []string{"observation", "world", "task", "edge"} {
		m.ObserveIngestDuration(0.001, cat)
	}
	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	hMF := findMF(mfs, "hermem_ingest_duration_seconds")
	if hMF == nil {
		t.Fatal("hIngest MetricFamily missing despite pre-warm; Vec not registered")
	}
	seenLabels := map[string]bool{}
	for _, child := range hMF.GetMetric() {
		for _, lp := range child.GetLabel() {
			if lp.GetName() == "category" {
				seenLabels[lp.GetValue()] = true
			}
		}
	}
	wantCategories := []string{"_init", "observation", "world", "task", "edge"}
	if len(seenLabels) != len(wantCategories) {
		t.Errorf("expected %d category labels after prewarm+4 observations, got %d (labels: %v)",
			len(wantCategories), len(seenLabels), seenLabels)
	}
	for _, want := range wantCategories {
		if !seenLabels[want] {
			t.Errorf("expected category=%q in registry, missing (got: %v)", want, seenLabels)
		}
	}
}

// TestHermemPrefixContract_KnownModesSet enforces the bounded-value-set
// contract on the hRetrieval `mode` label. Adding a new retrieval-side
// endpoint MUST extend knownModes in metrics.go AND bump the `want`
// slice below. Total time-series math: len(knownModes) * (10 durationBuckets
// + +Inf auto-added = 11 buckets + _sum + _count) per scrape.
func TestHermemPrefixContract_KnownModesSet(t *testing.T) {
	m := New()
	for _, mode := range []string{"search", "retrieve", "query", "response", "query_explain", "provenance"} {
		m.ObserveRetrievalDuration(0.001, mode)
	}
	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	hMF := findMF(mfs, "hermem_retrieval_duration_seconds")
	if hMF == nil {
		t.Fatal("hRetrieval MetricFamily missing despite pre-warm; Vec not registered")
	}
	seenLabels := map[string]bool{}
	for _, child := range hMF.GetMetric() {
		for _, lp := range child.GetLabel() {
			if lp.GetName() == "mode" {
				seenLabels[lp.GetValue()] = true
			}
		}
	}
	wantModes := []string{"_init", "search", "retrieve", "query", "response", "query_explain", "provenance"}
	if len(seenLabels) != len(wantModes) {
		t.Errorf("expected %d mode labels after prewarm+6 observations, got %d (labels: %v)",
			len(wantModes), len(seenLabels), seenLabels)
	}
	for _, want := range wantModes {
		if !seenLabels[want] {
			t.Errorf("expected mode=%q in registry, missing (got: %v)", want, seenLabels)
		}
	}
}

// TestHermemPrefixContract_KnownDetectorsSet enforces the bounded-value-set
// contract on the hContradiction `detector` label. Adding a new detector
// MUST extend knownDetectors in metrics.go AND bump the `want` slice below.
//
// Aligned with src/internal/contradiction's concrete detectors today:
// NewLexicalDetector + NewCompositeDetector. The "semantic" detector is
// planned (PHASE 2.4 comments) but not yet implemented, so it is NOT in the
// bounded value-set until it lands.
func TestHermemPrefixContract_KnownDetectorsSet(t *testing.T) {
	m := New()
	for _, d := range []string{"lexical", "composite"} {
		m.ObserveContradictionDuration(0.001, d)
	}
	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	hMF := findMF(mfs, "hermem_contradiction_duration_seconds")
	if hMF == nil {
		t.Fatal("hContradiction MetricFamily missing despite pre-warm; Vec not registered")
	}
	seenLabels := map[string]bool{}
	for _, child := range hMF.GetMetric() {
		for _, lp := range child.GetLabel() {
			if lp.GetName() == "detector" {
				seenLabels[lp.GetValue()] = true
			}
		}
	}
	wantDetectors := []string{"_init", "lexical", "composite"}
	if len(seenLabels) != len(wantDetectors) {
		t.Errorf("expected %d detector labels after prewarm+2 observations, got %d (labels: %v)",
			len(wantDetectors), len(seenLabels), seenLabels)
	}
	for _, want := range wantDetectors {
		if !seenLabels[want] {
			t.Errorf("expected detector=%q in registry, missing (got: %v)", want, seenLabels)
		}
	}
}

// TestHermemPrefixContract_KnownStrategiesSet enforces the bounded-value-set
// contract on the hRerank `strategy` label. Adding a new rerank strategy
// MUST extend knownStrategies in metrics.go AND bump the `want` slice below.
//
// Aligned with src/internal/ai/reranker.go:
//
//   - NewOllamaReranker / NewOpenAIReranker → "llm"
//   - NoopReranker / raw-cosine order → "cosine_only"
//   - Cross-encoder → planned "cross_encoder" (not implemented yet; bounded set
//     reserves the slot so dashboard authors know future it is intended)
func TestHermemPrefixContract_KnownStrategiesSet(t *testing.T) {
	m := New()
	for _, s := range []string{"llm_openai", "llm_ollama", "noop"} {
		m.ObserveRerankDuration(0.001, s)
	}
	mfs, err := m.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	hMF := findMF(mfs, "hermem_rerank_duration_seconds")
	if hMF == nil {
		t.Fatal("hRerank MetricFamily missing despite pre-warm; Vec not registered")
	}
	seenLabels := map[string]bool{}
	for _, child := range hMF.GetMetric() {
		for _, lp := range child.GetLabel() {
			if lp.GetName() == "strategy" {
				seenLabels[lp.GetValue()] = true
			}
		}
	}
	wantStrategies := []string{"_init", "llm_openai", "llm_ollama", "noop"}
	if len(seenLabels) != len(wantStrategies) {
		t.Errorf("expected %d strategy labels after prewarm+3 observations, got %d (labels: %v)",
			len(wantStrategies), len(seenLabels), seenLabels)
	}
	for _, want := range wantStrategies {
		if !seenLabels[want] {
			t.Errorf("expected strategy=%q in registry, missing (got: %v)", want, seenLabels)
		}
	}
}
