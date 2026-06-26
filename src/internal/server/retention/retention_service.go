// Package retention exposes retention.Service over HTTP. Synchronous
// POST /admin/retention/run returns the GCReport envelope body
// directly, regardless of HTTP status — bespoke envelope-as-body
// contract that bypasses server.mapStatus on the success/failure path.
// Long-lived auto-Run loop is wired separately by cli/serve.go.
//
// §3.2 — embeds shared.BaseHTTPService. DefaultPolicy stays as a
// shell-local field (per-shell snapshot semantics, not part of the
// base). Server.mapStatus doesn't apply to HandleRun because the
// handler has a NON-default success/failure body shape (it writes the
// GCReport envelope regardless of HTTP status); Wrap is still
// useful for the 405 path.
package retention

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/retention"
	"github.com/pavelveter/hermem/src/internal/server/shared"
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
	DefaultPolicy core.RetentionPolicy
	shared.BaseHTTPService
}

// New constructs the retention HTTP shell. DefaultPolicy is the snapshot
// taken at boot from cfg.Retention; cli/serve.go threads it from
// env.Cfg.Retention.
func New(svc *retention.Service, m *metrics.Metrics, refs *serverstate.Ref, defaultPolicy core.RetentionPolicy) *HTTPService {
	return &HTTPService{
		Svc:           svc,
		DefaultPolicy: defaultPolicy,
		BaseHTTPService: shared.BaseHTTPService{
			Metrics: m,
			Refs:    refs,
		},
	}
}

// Routes returns the single POST handler registered on the parent mux.
func (h *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/admin/retention/run": h.Wrap(h.HandleRun),
	}
}

// HandleRun synchronously fires RunOnce and writes the GCReport envelope
// directly (no wrapper struct). POST only; any other method gets 405
// with an Allow header.
//
// §3.2 — error-returning handler. The RunOnce error path keeps its
// pre-§3.2 envelope-as-body behaviour: when RunOnce returns an error
// the GCReport is still populated (PHASE 3.3 semantics) so the handler
// emits the rep on 500 then returns the err, which h.Wrap's mapStatus
// converts to 500 status (default fallback). Identical wire shape.
//
// Response shape — FLAT GCReport, NOT a wrapper. Metric: IncRetentionRun
// fires once per dispatch (HTTP 200 + 500 both fire).
func (h *HTTPService) HandleRun(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	h.Metrics.IncRetentionRun()
	rep, err := h.Svc.RunOnce(r.Context(), h.DefaultPolicy)
	if err != nil {
		// Write the partial report directly and return nil — NOT err.
		// Returning err here double-writes: the GCReport body is the
		// envelope-as-body PHASE 3.3 contract; if Wrap fires afterwards
		// it would IncErr + WriteError a SECOND JSON envelope on top of
		// the rep body, corrupting the response bytes. nil return means
		// Wrap no-ops. Pre-§3.2 the handler did not IncErr on this path
		// (only IncRetentionRun) because the body itself signals failure;
		// we preserve that exact metrics contract.
		httputil.WriteJSON(w, http.StatusInternalServerError, rep)
		return nil
	}
	httputil.WriteJSON(w, http.StatusOK, rep)
	return nil
}
