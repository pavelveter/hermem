// Package health provides the HTTP shell for the health-probe domain.
//
// PHASE 3.7 lifts /health, /health/live, /health/ready out of the
// server.AdminService into this dedicated HTTP shell. The transport-
// agnostic domain logic lives in src/internal/health/service.go;
// this package is a thin dispatcher that maps URL → HandlerFunc
// and delegates the probe logic to the domain Service.
package health

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/health"
	"github.com/pavelveter/hermem/src/internal/httputil"
)

// HTTPService is the HTTP shell for the health-probe domain.
// Holds a borrowed pointer to the transport-agnostic domain Service.
// No Metrics (health probes are not counted as application requests)
// and no Refs (health probes are schema-independent).
type HTTPService struct {
	Svc *health.Service
}

// New constructs an HTTPService wrapping the given domain Service.
func New(svc *health.Service) *HTTPService {
	return &HTTPService{Svc: svc}
}

// Routes returns the URL → handler mapping for this service.
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/health":       s.HandleHealth,
		"/health/live":  s.HandleHealthLive,
		"/health/ready": s.HandleHealthReady,
	}
}

// HandleHealth — GET /health. Always returns 200 OK.
func (s *HTTPService) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, s.Svc.Health())
}

// HandleHealthLive — GET /health/live. Always returns 200 OK.
func (s *HTTPService) HandleHealthLive(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, s.Svc.Live())
}

// HandleHealthReady — GET /health/ready. Pings the DB; returns
// 503 if unreachable. Maps the domain-layer healthy bool to HTTP
// status: true→200, false→503.
func (s *HTTPService) HandleHealthReady(w http.ResponseWriter, r *http.Request) {
	healthy, body := s.Svc.Ready(r.Context())
	code := http.StatusOK
	if !healthy {
		code = http.StatusServiceUnavailable
	}
	httputil.WriteJSON(w, code, body)
}
