package server

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/metrics"
)

// AdminService serves the /metrics Prometheus exposition endpoint.
//
// PHASE 3.6: /admin/re-embed moved to server/reembed.
// PHASE 3.7: /health/* moved to server/health.
// AdminService now owns only /metrics.
type AdminService struct {
	Metrics *metrics.Metrics
}

// NewAdminService constructs an AdminService with a metrics holder.
// m is the request-counter holder owned by Env.Metrics — the /metrics
// HTTP route is served by the same *Metrics that the rest of the
// handler suite bumps, so scraper and incrementer share a single
// source of truth.
func NewAdminService(m *metrics.Metrics) *AdminService {
	return &AdminService{Metrics: m}
}

// Routes returns the URL → handler mapping for this service.
//
// PHASE 3.1 lifted /connected-components + /communities → server/graph.
// PHASE 3.6 lifted /admin/re-embed → server/reembed.
// PHASE 3.7 lifted /health, /health/live, /health/ready → server/health.
// AdminService now owns only /metrics.
func (s *AdminService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/metrics": s.HandleMetrics,
	}
}

// HandleMetrics serves Prometheus-format metrics.
func (s *AdminService) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	s.Metrics.MetricsHandler(w, r)
}
