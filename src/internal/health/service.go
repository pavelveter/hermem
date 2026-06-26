package health

import (
	"context"
	"time"

	"github.com/pavelveter/hermem/src/internal/metrics"
)

type Check struct {
	Name     string
	Probe    func(ctx context.Context) error
	Timeout  time.Duration
	Severity string
}

type CheckResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
	Critical  bool   `json:"critical"`
}

type Status struct {
	Status  string                 `json:"status"`
	Latency int64                  `json:"latency_ms"`
	Checks  map[string]CheckResult `json:"checks,omitempty"`
	Ready   bool                   `json:"-"`
}

// HealthResponse is the wire shape returned by Service.Health. The
// Metrics field is populated only when the service was constructed
// with WithMetrics — nil-safe for the CLI / test fixture paths that
// don't carry a *metrics.Metrics around. Field names mirror the
// Prometheus metric names returned by metrics.Snapshot() so a
// consumer reading both surfaces sees consistent key names.
type HealthResponse struct {
	Status  string            `json:"status"`
	Metrics map[string]uint64 `json:"metrics,omitempty"`
}

type Service struct {
	checks  []Check
	metrics *metrics.Metrics
}

// New constructs a Service. The variadic Check list is the only
// required arg; metrics wiring is opt-in via WithMetrics so legacy
// CLI / test fixtures (which don't carry a *metrics.Metrics) keep
// working unchanged.
func New(checks ...Check) *Service {
	return &Service{checks: checks}
}

// WithMetrics attaches a *metrics.Metrics to the Service so that
// Health() includes a counter snapshot in the response. Returns
// the receiver for fluent chaining. Nil-safe: passing nil is a
// no-op (the field stays nil and Health() omits the metrics block).
//
// Call site: cli/serve.go passes env.Metrics; tests stay on plain
// health.New(...).
func (s *Service) WithMetrics(m *metrics.Metrics) *Service {
	if m != nil {
		s.metrics = m
	}
	return s
}

func (s *Service) Health() HealthResponse {
	resp := HealthResponse{Status: "ok"}
	if s.metrics != nil {
		resp.Metrics = s.metrics.Snapshot()
	}
	return resp
}

func (s *Service) Live() map[string]string {
	return map[string]string{"status": "ok"}
}

func (s *Service) Ready(ctx context.Context) Status {
	start := time.Now()
	st := Status{
		Status: "ok",
		Checks: make(map[string]CheckResult, len(s.checks)),
		Ready:  true,
	}

	for _, chk := range s.checks {
		checkStart := time.Now()

		probeCtx := ctx
		var cancel context.CancelFunc
		if chk.Timeout > 0 {
			probeCtx, cancel = context.WithTimeout(ctx, chk.Timeout)
		}

		err := chk.Probe(probeCtx)

		if cancel != nil {
			cancel()
		}

		r := CheckResult{
			OK:        err == nil,
			LatencyMs: time.Since(checkStart).Milliseconds(),
			Critical:  chk.Severity == "critical",
		}
		if err != nil {
			r.Error = err.Error()
		}

		st.Checks[chk.Name] = r
		if !r.OK && r.Critical {
			st.Ready = false
			st.Status = "degraded"
		}
	}

	st.Latency = time.Since(start).Milliseconds()
	return st
}
