// Package reembed_http exposes reembed.Service over HTTP.
//
// PHASE 3.6 — moves the /admin/re-embed route out of
// src/internal/server/admin_service.go into this new shell
// following the PHASE 3.1 + 3.2 + 3.3 + 3.4 + 3.5 transport-
// extraction pattern. The AdminService HTTP shell no longer
// owns /admin/re-embed — the reembed HTTP shell owns it
// exclusively. The /admin/re-embed URL stays byte-identical
// so existing clients see no drift between PHASE 3.5 and
// PHASE 3.6.
//
// §3.2 — embeds shared.BaseHTTPService for the cross-shell
// {Metrics, Refs} pair and routes via s.Wrap. No Refs because
// re-embed reads every entity directly from the DB (no schema
// gates). Matching the PHASE 3.5 timeline shell shape:
// {Svc, Metrics}, the minimum HTTPService dimension in the chain.
package reembed

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/reembed"
	"github.com/pavelveter/hermem/src/internal/server/shared"
)

// HTTPService is the transport shell for reembed.Service. Holds
// the borrowed reembed.Service pointer + observability only — no
// serverstate.Ref because re-embed read all entities directly
// from the DB (no schema gates).
type HTTPService struct {
	Svc *reembed.Service
	shared.BaseHTTPService
}

// New constructs a reembed HTTPService. Svc is required; the
// handler returns 500 if nil is dispatched (defensive against
// future zero-value wiring).
func New(svc *reembed.Service, m *metrics.Metrics) *HTTPService {
	return &HTTPService{
		Svc:             svc,
		BaseHTTPService: shared.BaseHTTPService{Metrics: m},
	}
}

// Routes returns the URL → handler mapping. Wired by Server in
// src/internal/server/server.go via the per-service Routes()
// protocol. /admin/re-embed moved here from AdminService in
// PHASE 3.6.
func (h *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/admin/re-embed": h.Wrap(h.HandleReEmbed),
	}
}

// HandleReEmbed — POST /admin/re-embed. Re-embeds every entity
// using the configured walk. Behaves identically to the
// pre-PHASE-3.6 server.AdminService.HandleReEmbed; only the
// underlying call changed from algo.ReEmbedAll to
// reembed.Service.ReEmbedAll.
//
// §3.2 — error-returning handler. Transport-level rejections
// (405, 400 dim required, etc.) WriteError + return nil;
// domain errors flow as err to h.Wrap.
func (h *HTTPService) HandleReEmbed(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	req, err := httputil.DecodeJSON[struct {
		BatchSize int    `json:"batch_size"`
		Dim       int    `json:"dim"`
		Model     string `json:"model"`
	}](w, r)
	if err != nil {
		return err
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 50
	}
	if req.Dim <= 0 {
		httputil.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "dim required", "invalid_input", "dim")
		return nil
	}
	result, err := h.Svc.ReEmbedAll(r.Context(), req.Dim, req.BatchSize, req.Model)
	if err != nil {
		return err
	}
	httputil.WriteJSON(w, http.StatusOK, result)
	return nil
}
