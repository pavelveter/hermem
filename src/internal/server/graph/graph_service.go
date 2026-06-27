// Package graph exposes graph.Service over HTTP. Three routes
// (/connected-components, /communities, /graph/verify). Dim is a
// shell-local snapshot (vec-dim from cfg; handlers don't read Refs).
//
// §3.2 — embeds shared.BaseHTTPService; Dim stays as a shell-local
// field (construction-time VecDim snapshot, not part of the base).
package graph

import (
	"net/http"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	graphsvc "github.com/pavelveter/hermem/src/internal/graph"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/server/shared"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService wires the graph analytics domain to the HTTP mux.
//
// Four deps:
//   - Svc     — domain Service (Components / Communities / Verify)
//   - Metrics — counter increments; never held by the domain Service
//   - Refs    — per-request schema source so SIGHUP reloads apply
//     without reconstructing the shell
//   - Dim     — vector dimension held at construction so the Verify
//     handler doesn't need to thread Cfg.VectorDim through routing;
//     the runtime loads it once from cfg and passes it via New()
type HTTPService struct {
	Svc *graphsvc.Service
	Dim int
	shared.BaseHTTPService
}

// New constructs an HTTPService. All four deps are required.
func New(svc *graphsvc.Service, m *metrics.Metrics, refs *serverstate.Ref, dim int) *HTTPService {
	return &HTTPService{
		Svc: svc,
		Dim: dim,
		BaseHTTPService: shared.BaseHTTPService{
			Metrics: m,
			Refs:    refs,
		},
	}
}

// Routes returns the URL → handler mapping for this shell.
//
// §3.2 — all three handlers route through s.Wrap.
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/connected-components": s.Wrap(s.HandleConnectedComponents),
		"/communities":          s.Wrap(s.HandleCommunities),
		"/graph/verify":         s.Wrap(s.HandleGraphVerify),
	}
}

// HandleConnectedComponents — GET /connected-components[?min_size=N].
//
// §3.2 — error-returning handler.
func (s *HTTPService) HandleConnectedComponents(w http.ResponseWriter, r *http.Request) error {
	s.Metrics.IncGraphComponents()
	minSize := httputil.ParseIntParam(r, "min_size", 2)
	comps, err := s.Svc.Components(r.Context(), minSize)
	if err != nil {
		return err
	}
	httputil.WriteJSON(w, http.StatusOK, comps)
	return nil
}

// HandleCommunities — GET /communities[?min_size=N&max_iterations=N].
//
// §3.2 — error-returning handler. The minSize filter + envelope
// composition stay in the shell (they're transport-layer concerns),
// while the Svc.Communities error path returns err so s.Wrap fires
// IncErr + WriteError after the observe duration is recorded.
func (s *HTTPService) HandleCommunities(w http.ResponseWriter, r *http.Request) error {
	s.Metrics.IncGraphCommunities()
	start := time.Now()
	defer func() { s.Metrics.ObserveGraphCommunitiesDuration(time.Since(start).Seconds()) }()
	minSize := httputil.ParseIntParam(r, "min_size", 2)
	maxIter := httputil.ParseIntParam(r, "max_iterations", 50)
	if maxIter <= 0 || maxIter > 200 {
		maxIter = 50
	}
	all, globalQ, err := s.Svc.Communities(r.Context(), maxIter)
	if err != nil {
		return err
	}
	filtered := make([]core.Community, 0)
	for _, c := range all {
		if c.Size >= minSize {
			filtered = append(filtered, c)
		}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"communities":          filtered,
		"global_modularity":    globalQ,
		"total_communities":    len(all),
		"filtered_communities": len(filtered),
	})
	return nil
}

// HandleGraphVerify — GET /graph/verify.
//
// §3.2 — error-returning handler. Defer the no-state pre-check to
// mapStatus by returning a sentinel error wrapping the literal
// message. The serverstate-is-nil condition is kept as a defensive
// 500 path in production even though it should not occur.
func (s *HTTPService) HandleGraphVerify(w http.ResponseWriter, r *http.Request) error {
	s.Metrics.IncGraphVerify()
	state := s.Refs.Load()
	if state == nil {
		// Defense-in-depth: production should always have a state
		// loaded; mapStatus falls back to 500 with err.Error().
		return shared.ErrNoServerState
	}
	report, err := s.Svc.Verify(r.Context(), state.Schema, s.Dim)
	if err != nil {
		return err
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"pass":     report.Pass(),
		"issues":   report.Issues,
		"rendered": report.String(),
	})
	return nil
}

// HandleGraphVerify's defensive state==nil branch returns
// shared.ErrNoServerState so that mapStatus routes to (500, "no
// server state").
