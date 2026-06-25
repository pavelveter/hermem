// Package retention_http exposes retention.Service over HTTP.
//
// PHASE 3.3 — adds one NEW route (POST /admin/retention/run) that fires
// retention.Service.RunOnce synchronously and returns a runResponse
// envelope wrapping its GCReport. No HTTP surface for retention existed
// pre-PHASE-3.3; the goroutine lived inside server.Server.Serve as a raw
// algo.GarbageCollector call and the only observability was slog.Info
// lines. HTTP clients now have their first read-write entry point into
// the archive sweep: the handler returns the full GCReport (started_at,
// finished_at, swept, error) so callers can verify the sweep outcome in
// the same request lifecycle.
//
// Authenticity: the handler is synchronous by design — callers want
// confirm-before-respond semantics so they can retry on partial archive.
// The long-lived auto-Run loop is wired by cli/serve.go via
// server.Server.Serve (unchanged drain order: HTTP → GC cancel → DB
// close).
package retention_http

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/retention"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService is the HTTP shell for retention.Service. Holds the borrowed
// Service pointer + observability + a default policy snapshot taken at
// boot from cfg.Retention (mirrors PHASE 3.1 graph's VectorDim
// construction-time commitment). SIGHUP-driven policy swaps are not
// propagated — by design, matching the pre-PHASE-3.3 closure-capture
// behaviour inside server.Server.Serve.
type HTTPService struct {
	Svc           *retention.Service
	Metrics       *metrics.Metrics
	Refs          *serverstate.Ref
	DefaultPolicy core.RetentionPolicy
}

// New constructs the retention HTTP shell. DefaultPolicy is the snapshot
// taken at boot from cfg.Retention; cli/serve.go threads it from
// env.Cfg.Retention.
func New(svc *retention.Service, m *metrics.Metrics, refs *serverstate.Ref, defaultPolicy core.RetentionPolicy) *HTTPService {
	return &HTTPService{Svc: svc, Metrics: m, Refs: refs, DefaultPolicy: defaultPolicy}
}

// Routes returns the single POST handler registered on the parent mux.
func (h *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/admin/retention/run": h.HandleRun,
	}
}

// HandleRun synchronously fires RunOnce and writes the GCReport envelope
// directly (no wrapper struct). POST only; any other method gets 405
// with an Allow header. Auth flows through the canonical
// APIKeyMiddleware in server.Server.mount — this handler does not
// duplicate auth logic.
//
// Response shape — FLAT GCReport, NOT a wrapper. PHASE 3.3 review
// flagged the prior `{report: GCReport, error: "..."}` envelope as
// double-error (GCReport.Error is already populated on RunOnce
// error). Flat shape mirrors PHASE 3.1 graph /communities envelope
// (`{communities, global_modularity, total_communities,
// filtered_communities}`) and PHASE 3.2 migration /db/schema envelope
// (`{stored, current, drift_detected}`). HTTP 500 still writes the
// GCReport body so the operator can inspect partial archive state via
// the same shape.
//
// Metric: IncRetentionRun fires once per dispatch (HTTP 200 + 500
// both fire because operators verify the sweep via the GCReport on
// the body, not via the HTTP status). See metrics.IncRetentionRun for
// counter semantics.
func (h *HTTPService) HandleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	h.Metrics.IncRetentionRun()
	rep, err := h.Svc.RunOnce(r.Context(), h.DefaultPolicy)
	if err != nil {
		httputil.WriteJSON(w, http.StatusInternalServerError, rep)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, rep)
}
