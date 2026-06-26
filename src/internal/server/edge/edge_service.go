// Package edge_http exposes edge.Service over HTTP.
//
// PHASE 3.5 — moves the /edge route out of src/internal/server/memory/
// into this new shell following the PHASE 3.1 + 3.2 + 3.3 + 3.4
// transport-extraction pattern. The memory HTTP shell no longer owns
// /edge — the edge HTTP shell owns it exclusively. The /edge URL
// stays byte-identical so existing clients see no URL drift between
// PHASE 3.4 and PHASE 3.5.
//
// §3.2 — embeds shared.BaseHTTPService; h.Wrap folds the
// IncErr + WriteError(500,...) boilerplate at the Svc.AddEdge error
// path. Inline schema-conflict detection stays in shared (separate
// concern from Wrap-status mapping).
package edge

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	edgedomain "github.com/pavelveter/hermem/src/internal/edge"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/server/shared"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService is the transport shell for edge.Service. Holds the
// borrowed edge.Service pointer + observability + the serverstate.Ref
// for schema-conflict checks (Same pattern as PHASE 3.1 graph's
// HTTPService + PHASE 3.3 retention's HTTPService + PHASE 3.4
// ingest's HTTPService).
type HTTPService struct {
	Svc *edgedomain.Service
	shared.BaseHTTPService
}

// New constructs an edge HTTPService. The Svc field is required (no
// fallback): callers that want a 405 without a domain touch wire a
// nil Svc and the handler returns 500 to be safe.
func New(svc *edgedomain.Service, m *metrics.Metrics, refs *serverstate.Ref) *HTTPService {
	return &HTTPService{
		Svc: svc,
		BaseHTTPService: shared.BaseHTTPService{
			Metrics: m,
			Refs:    refs,
		},
	}
}

// Routes returns the URL → handler mapping. Wired by Server in
// src/internal/server/server.go via the per-service Routes() protocol.
// /edge POST moved here from the memory shell in PHASE 3.5.
//
// §3.2 — handler is wrapped so IncErr + WriteError(500,...) is folded.
func (h *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/edge": h.Wrap(h.HandleEdge),
	}
}

// HandleEdge — POST /edge. Persists a single relation edge,
// optionally auto-creating missing endpoint entities
// (AutoCreate=true). Behaves identically to the pre-PHASE-3.5
// server/memory HandleEdge; only the underlying domain Service
// pointer changed (memory → edge).
//
// §3.2 — error-returning handler. Transport-level rejections
// (405, missing fields, unknown relation_type) WriteError +
// return nil; domain errors flow as err so h.Wrap fires the
// IncErr + mapStatus write. The IsSchemaErr-as-422 mapping is
// now done by server.mapStatus' CodeInvalidSchema branch, so
// the pre-§3.2 inline `status := 422 if IsSchemaErr else 500`
// collapse is no longer needed.
func (h *HTTPService) HandleEdge(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	req, err := httputil.DecodeJSON[core.EdgeRequest](w, r)
	if err != nil {
		return err
	}
	if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
		httputil.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "source_id, target_id, relation_type required", "invalid_input", "")
		return nil
	}
	state := h.Refs.Load()
	if !state.ValidRelationTypes[req.RelationType] {
		httputil.WriteError(w, http.StatusUnprocessableEntity, "unknown relation_type: "+req.RelationType)
		return nil
	}
	if shared.RejectSchemaConflict(w, state.Generation, h.Refs, h.Metrics) {
		return nil
	}
	if err := h.Svc.AddEdge(r.Context(), req, state.Schema); err != nil {
		return err
	}
	h.Metrics.IncEdge()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}
