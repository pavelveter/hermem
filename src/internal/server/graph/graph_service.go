// Package graph hosts the HTTP shell for the graph analytics domain.
//
// PHASE 3.1 introduces this shell as part of the god-object demolition
// pattern established by PHASE 2.x. The shell lifts two routes out of
// the central AdminService (/connected-components, /communities) and
// adds one NEW route (/graph/verify) that exposes algo.VerifyGraph over
// HTTP. AdminService retains /health/*, /metrics, and /admin/re-embed,
// which are out of the graph domain.
package graph

import (
	"net/http"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	graphsvc "github.com/pavelveter/hermem/src/internal/graph"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
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
	Svc     *graphsvc.Service
	Metrics *metrics.Metrics
	Refs    *serverstate.Ref
	Dim     int
}

// New constructs an HTTPService. All four deps are required.
func New(svc *graphsvc.Service, m *metrics.Metrics, refs *serverstate.Ref, dim int) *HTTPService {
	return &HTTPService{Svc: svc, Metrics: m, Refs: refs, Dim: dim}
}

// Routes returns the URL → handler mapping for this shell.
//
// Three endpoints:
//   - /connected-components (moved from AdminService in PHASE 3.1)
//   - /communities          (moved from AdminService in PHASE 3.1)
//   - /graph/verify         (NEW — algo.VerifyGraph exposed over HTTP)
//
// Pre-PHASE-3.1 /connected-components and /communities lived in
// AdminService.Routes(). They are removed there.
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/connected-components": s.HandleConnectedComponents,
		"/communities":          s.HandleCommunities,
		"/graph/verify":         s.HandleGraphVerify,
	}
}

// HandleConnectedComponents — GET /connected-components[?min_size=N].
// Returns the connected-component list (size ≥ min_size, default 2)
// as a raw JSON array. Matches the pre-PHASE-3.1 envelope exactly so
// existing curl clients / dashboards are unchanged.
func (s *HTTPService) HandleConnectedComponents(w http.ResponseWriter, r *http.Request) {
	s.Metrics.IncGraphComponents()
	minSize := httputil.ParseIntParam(r, "min_size", 2)
	comps, err := s.Svc.Components(r.Context(), minSize)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, comps)
}

// HandleCommunities — GET /communities[?min_size=N&max_iterations=N].
// Runs Louvain detection (default 50 iterations, clamped ≤200) and
// returns the total / filtered envelope so callers can compare
// unfiltered counts against min_size-filtered counts.
//
// minSize filtering lives HERE (not in the domain Service) so the
// envelope can report total_communities AND filtered_communities side
// by side. The domain Service returns the full list.
func (s *HTTPService) HandleCommunities(w http.ResponseWriter, r *http.Request) {
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
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var filtered []core.Community
	for _, c := range all {
		if c.Size >= minSize {
			filtered = append(filtered, c)
		}
	}
	if filtered == nil {
		filtered = []core.Community{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"communities":          filtered,
		"global_modularity":    globalQ,
		"total_communities":    len(all),
		"filtered_communities": len(filtered),
	})
}

// HandleGraphVerify — GET /graph/verify. Returns a JSON envelope that
// mirrors the cli verify contract: {pass:bool, issues:[], rendered:
// "human-readable report"}. PHASE 3.1 NEW route — exposes alg_o.
// VerifyGraph over HTTP. The `pass` boolean derives from
// `len(report.Issues) == 0` so dashboards can decide red/green
// without re-running the algorithm.
func (s *HTTPService) HandleGraphVerify(w http.ResponseWriter, r *http.Request) {
	s.Metrics.IncGraphVerify()
	state := s.Refs.Load()
	if state == nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, "no server state")
		return
	}
	report, err := s.Svc.Verify(r.Context(), state.Schema, s.Dim)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"pass":     report.Pass(),
		"issues":   report.Issues,
		"rendered": report.String(),
	})
}
