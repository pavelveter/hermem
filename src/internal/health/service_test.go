package health_test

import (
	"context"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/health"
	metricspkg "github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/store"
)

func newHealthFixture(t *testing.T) *health.Service {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return health.New(health.DBProbe(db))
}

func TestNewService_Success(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := health.New(health.DBProbe(db))
	if svc == nil {
		t.Fatal("New returned nil Service")
	}
}

func TestHealth_ReturnsOk(t *testing.T) {
	svc := newHealthFixture(t)
	result := svc.Health()
	if result.Status != "ok" {
		t.Fatalf("want status=ok, got %+v", result)
	}
}

// TestHealth_WithMetrics_PopulatesSnapshot verifies the §7.1 wire
// contract: when a Service is constructed with WithMetrics, Health()
// includes a metrics sub-map keyed by the same Prometheus metric
// names that /metrics emits. A non-empty map after at least one
// IncStore pins the snapshot path; nil-safe (no metrics wired) is
// the inverse case asserted by the un-wired test above.
func TestHealth_WithMetrics_PopulatesSnapshot(t *testing.T) {
	svc := newHealthFixture(t)
	m := metricspkg.New()
	m.IncStore()
	svc.WithMetrics(m)
	result := svc.Health()
	if result.Status != "ok" {
		t.Fatalf("want status=ok, got %+v", result)
	}
	if result.Metrics == nil {
		t.Fatal("want Metrics sub-map populated, got nil")
	}
	if got, want := result.Metrics["hermem_store_total"], uint64(1); got != want {
		t.Fatalf("hermem_store_total: want %d, got %d", want, got)
	}
	// Pin a few unrelated counters at zero so a future refactor that
	// accidentally drops a key from Snapshot() fails this assertion
	// rather than silently passing a partial sub-map.
	for _, k := range []string{
		"hermem_search_total", "hermem_ingest_total", "hermem_errors_total",
		"hermem_task_create_total", "hermem_retention_run_total",
	} {
		if _, ok := result.Metrics[k]; !ok {
			t.Errorf("Snapshot missing key %q (sub-map: %+v)", k, result.Metrics)
		}
	}
}

// TestHealth_WithoutMetrics_OmitsSnapshot pins the nil-safe branch:
// a Service built without WithMetrics() returns a HealthResponse
// whose Metrics field is nil (the JSON `omitempty` tag strips it
// from the wire). Pre-§7.1 the return type was map[string]string
// so this assertion was implicit; post-§7.1 it's a struct field
// that needs an explicit nil-check.
func TestHealth_WithoutMetrics_OmitsSnapshot(t *testing.T) {
	svc := newHealthFixture(t)
	result := svc.Health()
	if result.Metrics != nil {
		t.Fatalf("want nil Metrics when no metrics wired, got %+v", result.Metrics)
	}
}

func TestLive_ReturnsOk(t *testing.T) {
	svc := newHealthFixture(t)
	result := svc.Live()
	if result["status"] != "ok" {
		t.Fatalf("want status=ok, got %v", result)
	}
}

func TestReady_OK(t *testing.T) {
	svc := newHealthFixture(t)
	status := svc.Ready(context.Background())
	if !status.Ready {
		t.Fatalf("want Ready=true, got status=%s checks=%v", status.Status, status.Checks)
	}
	if status.Status != "ok" {
		t.Fatalf("want status=ok, got %v", status)
	}
}

func TestReady_DBError(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	db.Close()
	svc := health.New(health.DBProbe(db))
	status := svc.Ready(context.Background())
	if status.Ready {
		t.Fatalf("want Ready=false, got %v", status)
	}
	if status.Status != "degraded" {
		t.Fatalf("want status=degraded, got %v", status)
	}
}

func TestReady_MultipleChecks_Aggregation(t *testing.T) {
	svc := health.New(
		health.Check{
			Name:     "ok_check",
			Probe:    func(ctx context.Context) error { return nil },
			Timeout:  time.Second,
			Severity: "critical",
		},
		health.Check{
			Name:     "fail_check",
			Probe:    func(ctx context.Context) error { return nil },
			Timeout:  time.Second,
			Severity: "critical",
		},
	)
	status := svc.Ready(context.Background())
	if !status.Ready {
		t.Fatalf("want Ready=true, got %v", status)
	}
}

func TestReady_CriticalFail_SetsDegraded(t *testing.T) {
	svc := health.New(
		health.Check{
			Name:     "ok_check",
			Probe:    func(ctx context.Context) error { return nil },
			Timeout:  time.Second,
			Severity: "critical",
		},
		health.Check{
			Name:     "fail_check",
			Probe:    func(ctx context.Context) error { return assertError{} },
			Timeout:  time.Second,
			Severity: "critical",
		},
	)
	status := svc.Ready(context.Background())
	if status.Ready {
		t.Fatal("want Ready=false when critical check fails")
	}
	if status.Status != "degraded" {
		t.Fatalf("want status=degraded, got %s", status.Status)
	}
}

type assertError struct{}

func (assertError) Error() string { return "assert error" }

func TestReady_WarningFail_StillReady(t *testing.T) {
	svc := health.New(
		health.Check{
			Name:     "critical_ok",
			Probe:    func(ctx context.Context) error { return nil },
			Timeout:  time.Second,
			Severity: "critical",
		},
		health.Check{
			Name:     "warning_fail",
			Probe:    func(ctx context.Context) error { return assertError{} },
			Timeout:  time.Second,
			Severity: "warning",
		},
	)
	status := svc.Ready(context.Background())
	if !status.Ready {
		t.Fatal("want Ready=true when only warning checks fail")
	}
}

func TestReady_TimeoutRespected(t *testing.T) {
	svc := health.New(
		health.Check{
			Name: "slow_check",
			Probe: func(ctx context.Context) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Second):
					return nil
				}
			},
			Timeout:  10 * time.Millisecond,
			Severity: "critical",
		},
	)
	status := svc.Ready(context.Background())
	if status.Ready {
		t.Fatal("want Ready=false when check times out")
	}
	if status.Status != "degraded" {
		t.Fatalf("want status=degraded, got %s", status.Status)
	}
	r := status.Checks["slow_check"]
	if r.OK {
		t.Fatal("want check result OK=false after timeout")
	}
}
