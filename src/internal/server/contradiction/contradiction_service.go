// Package contradiction exposes contradiction.Service over HTTP. One
// route (GET /contradictions[?id=X]); no Refs (DB-only domain).
//
// §3.2 — embeds shared.BaseHTTPService for the cross-shell {Metrics,
// Refs} pair and routes via s.Wrap so the IncErr + WriteError(500,...)
// boilerplate is collapsed into a single `return err` at every
// domain-call site. Constructor signature is left unchanged
// (cli/serve.go + integration tests are not touched).
package contradiction

import (
	"net/http"

	contradict "github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/server/shared"
)

// HTTPService is the transport shell for the contradiction domain.
//
// Holds the domain Service (Svc) plus the embedded BaseHTTPService
// (Metrics promoted). No Refs because the contradiction domain is
// DB-only and has no schema gates — s.Refs is nil and handlers
// never read it.
type HTTPService struct {
	Svc *contradict.Service
	shared.BaseHTTPService
}

// New constructs an HTTPService. In production cli/serve.go wires the
// domain Service from env.DB via contradict.New(env.DB); tests
// construct the domain Service inline for fast in-memory fixtures.
//
// §3.2 — constructor signature unchanged; the Metrics arg is now
// captured via shared.BaseHTTPService field instead of a top-level
// Metrics field. Field-promotion keeps `s.Metrics.IncXxx(...)` working
// in handlers.
func New(svc *contradict.Service, m *metrics.Metrics) *HTTPService {
	return &HTTPService{
		Svc:             svc,
		BaseHTTPService: shared.BaseHTTPService{Metrics: m},
	}
}

// Routes returns the URL → handler mapping.
//
// GET /contradictions is the only endpoint after PHASE 2.3. §3.2 wires
// the handler through s.Wrap so the IncErr + WriteError(500,...)
// pattern is collapsed.
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/contradictions": s.Wrap(s.HandleContradictions),
	}
}

// HandleContradictions — GET /contradictions[?id=X].
//
// Optional ?id=X narrows the result to entityID=X either as source or
// as target. Empty DB returns `[]`, not `null` — the domain Service
// normalizes the nil→empty slice so the JSON envelope stays consistent.
//
// §3.2 — signature returns error so the success path is two lines long.
// The non-GET 405 rejection is a transport-level WriteError + nil
// return; only the domain-error path returns err so s.Wrap fires the
// IncErr + MapError write.
func (s *HTTPService) HandleContradictions(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	id := r.URL.Query().Get("id")
	pairs, err := s.Svc.List(r.Context(), id)
	if err != nil {
		return err
	}
	httputil.WriteJSON(w, http.StatusOK, pairs)
	return nil
}
