// Package contradiction hosts the HTTP transport for the contradiction
// subsystem.
//
// Domain logic lives in src/internal/contradiction (Service); this
// package owns transport-only concerns: JSON encoding, method check,
// metric increments, and the route registry for GET /contradictions[?id=X].
//
// Following the same pattern as PHASE 2.1's MemoryService extraction and
// PHASE 2.2's RetrievalService extraction: HTTPService is a thin shell
// — parse → call Service.List → write envelope. The domain Service has
// no knowledge of HTTP, httputil, serverstate, or metrics.
package contradiction

import (
	"net/http"

	contradict "github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
)

// HTTPService is the transport shell for the contradiction domain.
//
// Holds the domain Service (Svc) and the metrics counters (Metrics).
// Nothing else — the domain Service is intentionally DB-only so the HTTP
// shell doesn't need to plumb any other transport deps (no schema, no
// state, no embedder, no LLM extractor).
type HTTPService struct {
	Svc     *contradict.Service
	Metrics *metrics.Metrics
}

// New constructs an HTTPService. In production cli/serve.go wires the
// domain Service from env.DB via contradict.NewService(env.DB); tests
// construct the domain Service inline for fast in-memory fixtures.
func New(svc *contradict.Service, m *metrics.Metrics) *HTTPService {
	return &HTTPService{Svc: svc, Metrics: m}
}

// Routes returns the URL → handler mapping.
//
// GET /contradictions is the only endpoint after PHASE 2.3 — the route
// previously lived in retrieval_service.go::Routes() behind the
// temporary retrieval.Service.DB() accessor; PHASE 2.3 owns it firmly
// in this pkg.
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/contradictions": s.HandleContradictions,
	}
}

// HandleContradictions — GET /contradictions[?id=X].
//
// Optional ?id=X narrows the result to entityID=X either as source or
// as target. Empty DB returns `[]`, not `null` — the domain Service
// normalizes the nil→empty slice so the JSON envelope stays consistent.
//
// Errors map to:
//   - 405 — non-GET method
//   - 500 — domain error (IncErr + WriteError)
//
// The pre-PHASE-2.3 HTTP shell had identical behavior; preserve it
// verbatim per PHASE 2.3's "no behavior drift" rule.
func (s *HTTPService) HandleContradictions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := r.URL.Query().Get("id")
	pairs, err := s.Svc.List(r.Context(), id)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, pairs)
}
