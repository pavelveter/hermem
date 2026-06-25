package server

import (
	"database/sql"
	"net/http"

	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
)

// AdminService handles health and system stat endpoints.
//
// Handlers in this group do NOT depend on schema state.
//
// PHASE 3.6: /admin/re-embed moved to src/internal/server/reembed/;
// the reembed.Service is now a standalone transport-agnostic domain
// following the PHASE 3.1–3.5 pattern (flat pkg + companion HTTP
// shell). AdminService owns only /health/* + /metrics now.
type AdminService struct {
	DB      *sql.DB
	Metrics *metrics.Metrics
}

// NewAdminService constructs an AdminService. After PHASE 3.6 only
// db + metrics are kept; VI + Embedder + Refs were only used by
// the now-extracted HandleReEmbed. m is the request-counter holder
// owned by Env.Metrics — every handler that bumps a counter
// (BumpsIncErr etc.) reaches it through this field. The /metrics HTTP
// route is served by the same m via the closure in Routes() below.
func NewAdminService(db *sql.DB, m *metrics.Metrics) *AdminService {
	return &AdminService{DB: db, Metrics: m}
}

// Routes returns the URL → handler mapping for this service.
//
// PHASE 3.1 lifted /connected-components + /communities out into the
// new server/graph HTTP shell. PHASE 3.6 lifted /admin/re-embed out
// into the new server/reembed HTTP shell. AdminService now owns
// only health probes + /metrics.
func (s *AdminService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/health":       s.HandleHealth,
		"/health/live":  s.HandleHealthLive,
		"/health/ready": s.HandleHealthReady,
		"/metrics":      s.HandleMetrics,
	}
}

// HandleMetrics serves Prometheus-format metrics. Routes to
// s.Metrics.MetricsHandler so the exposed counters are the SAME
// atomic.Int64 fields that the rest of the handler suite bumps —
// scraper and incrementer share a single *Metrics, no parallel
// state path that could drift.
func (s *AdminService) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	s.Metrics.MetricsHandler(w, r)
}

func (s *AdminService) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *AdminService) HandleHealthLive(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *AdminService) HandleHealthReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{"database": "ok"}
	if err := s.DB.PingContext(r.Context()); err != nil {
		checks["database"] = "unreachable: " + err.Error()
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"status": "degraded", "checks": checks})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "checks": checks})
}

// HandleReEmbed — POST /admin/re-embed. Moved to
// src/internal/server/reembed/reembed_service.go in PHASE 3.6.
// The reembed.Service owns the transport-agnostic reembedding
// orchestrator; the HTTP shell delegates to that service.
// HandleReEmbed removed here — AdminService no longer owns
// the re-embed route.
