package server

import (
	"database/sql"
	"net/http"

	"github.com/pavelveter/hermem/src/internal/algo"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	"github.com/pavelveter/hermem/src/internal/store"
)

// AdminService handles health, system stat endpoints, and admin operations.
//
// Handlers in this group do NOT depend on schema state — they read directly
// from DB / VI / Embedder / Metrics. The schema-aware endpoints (store, edge,
// ingest, etc.) live in retrieval/task/memory services.
type AdminService struct {
	DB       *sql.DB
	VI       core.VectorIndex
	Embedder core.Embedder
	Metrics  *metrics.Metrics
	Refs     *serverstate.Ref
}

// NewAdminService constructs an AdminService. m is the request-counter
// holder owned by Env.Metrics — every handler that bumps a counter
// (BumpsIncErr etc.) reaches it through this field. The /metrics HTTP
// route is served by the same m via the closure in Routes() below, so a
// scrape AND a handler-driven increment observe the SAME atomic.Int64
// fields — no observable drift between the scraper's view and the
// handlers' contribution.
func NewAdminService(db *sql.DB, vi core.VectorIndex, embedder core.Embedder, m *metrics.Metrics, refs *serverstate.Ref) *AdminService {
	return &AdminService{DB: db, VI: vi, Embedder: embedder, Metrics: m, Refs: refs}
}

// Routes returns the URL → handler mapping for this service.
func (s *AdminService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/health":               s.HandleHealth,
		"/health/live":          s.HandleHealthLive,
		"/health/ready":         s.HandleHealthReady,
		"/metrics":              s.HandleMetrics,
		"/connected-components": s.HandleConnectedComponents,
		"/communities":          s.HandleCommunities,
		"/admin/re-embed":       s.HandleReEmbed,
	}
}

// HandleMetrics serves Prometheus-format metrics. Routes to
// s.Metrics.MetricsHandler so the exposed counters are the SAME
// atomic.Int64 fields that the rest of the handler suite bumps —
// scraper and incrementer share a single *Metrics, no parallel
// state path that could drift.
func (s *AdminService) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	s.Metrics.MetricsHandler(w, r)
}

func (s *AdminService) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *AdminService) HandleHealthLive(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *AdminService) HandleHealthReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{"database": "ok"}
	if err := s.DB.PingContext(r.Context()); err != nil {
		checks["database"] = "unreachable: " + err.Error()
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"status": "degraded", "checks": checks})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "checks": checks})
}

func (s *AdminService) HandleConnectedComponents(w http.ResponseWriter, r *http.Request) {
	minSize := httputil.ParseIntParam(r, "min_size", 2)
	components, err := store.FindConnectedComponents(s.DB, minSize)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if components == nil {
		components = []core.ConnectedComponent{}
	}
	httputil.WriteJSON(w, http.StatusOK, components)
}

func (s *AdminService) HandleCommunities(w http.ResponseWriter, r *http.Request) {
	minSize := httputil.ParseIntParam(r, "min_size", 2)
	maxIter := httputil.ParseIntParam(r, "max_iterations", 50)
	if maxIter <= 0 || maxIter > 200 {
		maxIter = 50
	}
	all, globalQ, err := store.DetectCommunities(s.DB, maxIter)
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

func (s *AdminService) HandleReEmbed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req struct {
		BatchSize int    `json:"batch_size"`
		Dim       int    `json:"dim"`
		Model     string `json:"model"`
	}
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 50
	}
	if req.Dim <= 0 {
		httputil.WriteError(w, http.StatusBadRequest, "dim required")
		return
	}
	result, err := algo.ReEmbedAll(r.Context(), s.DB, s.VI, s.Embedder, req.Dim, req.BatchSize, req.Model)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}
