package health

import (
	"context"
	"time"
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

type Service struct {
	checks []Check
}

func New(checks ...Check) *Service {
	return &Service{checks: checks}
}

func (s *Service) Health() map[string]string {
	return map[string]string{"status": "ok"}
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
