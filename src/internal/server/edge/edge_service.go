// Package edge_http exposes edge.Service over HTTP.
//
// PHASE 3.5 — moves the /edge route out of src/internal/server/memory/
// into this new shell following the PHASE 3.1 + 3.2 + 3.3 + 3.4
// transport-extraction pattern. The memory HTTP shell no longer owns
// /edge — the edge HTTP shell owns it exclusively. The /edge URL
// stays byte-identical so existing clients see no URL drift between
// PHASE 3.4 and PHASE 3.5.
package edge_http

import (
	"errors"
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	edgedomain "github.com/pavelveter/hermem/src/internal/edge"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService is the transport shell for edge.Service. Holds the
// borrowed edge.Service pointer + observability + the serverstate.Ref
// for schema-conflict checks (Same pattern as PHASE 3.1 graph's
// HTTPService + PHASE 3.3 retention's HTTPService + PHASE 3.4
// ingest's HTTPService).
type HTTPService struct {
	Svc     *edgedomain.Service
	Metrics *metrics.Metrics
	Refs    *serverstate.Ref
}

// New constructs an edge HTTPService. The Svc field is required (no
// fallback): callers that want a 405 without a domain touch wire a
// nil Svc and the handler returns 500 to be safe.
func New(svc *edgedomain.Service, m *metrics.Metrics, refs *serverstate.Ref) *HTTPService {
	return &HTTPService{Svc: svc, Metrics: m, Refs: refs}
}

// Routes returns the URL → handler mapping. Wired by Server in
// src/internal/server/server.go via the per-service Routes() protocol.
// /edge POST moved here from the memory shell in PHASE 3.5.
func (h *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/edge": h.HandleEdge,
	}
}

// rejectSchemaConflict writes the canonical 409 envelope and returns
// true if the schema generation observed at handler entry differs from
// the live Refs (SIGHUP swapped mid-request). Identical semantics to
// the memory shell's pre-PHASE-3.5 rejectSchemaConflict — copy-pasted
// rather than promoted to a shared helper because each shell owns its
// Refs read and the IncSchemaConflict-vs-IncErr separation contract.
func (h *HTTPService) rejectSchemaConflict(w http.ResponseWriter, gen uint64) bool {
	if !h.Refs.IsStale(gen) {
		return false
	}
	h.Metrics.IncSchemaConflict()
	httputil.WriteErrorWithCode(w, http.StatusConflict,
		"schema changed during request; retry",
		"schema_conflict", "")
	return true
}

// isSchemaErr reports whether err is an edgedomain.ErrInvalidSchema so
// the handler maps the domain's semantic validation failure to 422 —
// matching the pre-PHASE-3.5 envelope exactly so existing clients and
// tests don't see a status drift.
func isSchemaErr(err error) bool {
	if err == nil {
		return false
	}
	var ise *edgedomain.ErrInvalidSchema
	return errors.As(err, &ise)
}

// HandleEdge — POST /edge. Persists a single relation edge,
// optionally auto-creating missing endpoint entities
// (AutoCreate=true). Behaves identically to the pre-PHASE-3.5
// server/memory HandleEdge; only the underlying domain Service
// pointer changed (memory → edge).
func (h *HTTPService) HandleEdge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.EdgeRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	// Pre-PHASE-3.5 the missing-field check happened inline at the HTTP
	// layer — kept here verbatim so existing /edge clients continue to
	// see 400 for malformed bodies. The domain edge.Service.AddEdge
	// also enforces the same fields as defense-in-depth.
	if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
		httputil.WriteError(w, http.StatusBadRequest, "source_id, target_id, relation_type required")
		return
	}
	state := h.Refs.Load()
	if !state.ValidRelationTypes[req.RelationType] {
		httputil.WriteError(w, http.StatusUnprocessableEntity, "unknown relation_type: "+req.RelationType)
		return
	}
	if h.rejectSchemaConflict(w, state.Generation) {
		return
	}
	if err := h.Svc.AddEdge(r.Context(), req, state.Schema); err != nil {
		h.Metrics.IncErr()
		status := http.StatusInternalServerError
		if isSchemaErr(err) {
			status = http.StatusUnprocessableEntity
		}
		httputil.WriteError(w, status, err.Error())
		return
	}
	h.Metrics.IncEdge()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
