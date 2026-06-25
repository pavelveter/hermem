// Package health owns the transport-agnostic health-probe domain.
//
// PHASE 3.7 lifts the /health, /health/live, /health/ready handlers out
// of the server.AdminService god-object into its own flat pkg following
// the PHASE 3.1–3.6 pattern: flat pkg, stateless Service, no HTTP / CLI
// coupling. The HTTP shell lives in src/internal/server/health/.
//
// Health + Live are stateless (always ok); Ready pings the DB and
// returns degraded if unreachable. AdminService no longer owns health
// probes — it keeps only /metrics (a one-route Prometheus wrapper).
package health

import (
	"context"
	"database/sql"
)

// Service is the transport-agnostic health-probe domain.
// Holds db for the Ready probe's PingContext call; Health
// and Live are pure stateless passthroughs.
type Service struct {
	db *sql.DB
}

// New constructs a health Service. db is required — a nil db
// would make Ready always return degraded.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// Health returns the canonical liveness probe result.
// Always ok — the process is running.
func (s *Service) Health() map[string]string {
	return map[string]string{"status": "ok"}
}

// Live returns the Kubernetes-style liveness result.
// Always ok — identical to Health; separate route for
// orchestrator-level probe separation.
func (s *Service) Live() map[string]string {
	return map[string]string{"status": "ok"}
}

// Ready returns the readiness probe result. Pings the DB;
// returns degraded if PingContext fails.
func (s *Service) Ready(ctx context.Context) (int, map[string]interface{}) {
	checks := map[string]string{"database": "ok"}
	if err := s.db.PingContext(ctx); err != nil {
		checks["database"] = "unreachable: " + err.Error()
		return 503, map[string]interface{}{"status": "degraded", "checks": checks}
	}
	return 200, map[string]interface{}{"status": "ok", "checks": checks}
}
