// Package retrieval hosts the read-only HTTP transport for the retrieval
// subsystem. Domain logic lives in src/internal/retrieval (Service);
// this package owns transport-only concerns: JSON encoding, method
// checks, request-body limits, schema-conflict guards where applicable,
// and metric increments.
//
// /contradictions moved to src/internal/server/contradiction in
// PHASE 2.3 (ContradictionService extraction). It is no longer in
// this HTTP shell's Routes() registry.
//
// Following the same pattern as PHASE 2.1's MemoryService extraction:
// HTTPService is a thin shell — parse → validate → call RetSvc.* →
// write envelope. The domain service has no knowledge of HTTP,
// httputil, serverstate.Ref, or metrics.
package retrieval

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService is the transport shell for the read-side domain.
//
// Holds the domain Service (RetSvc), the metrics counters (Metrics),
// and the serverstate.Ref for state-conflict reads (state.Load() at
// request time to seed RetrieveContextOptions per the SIGHUP path).
type HTTPService struct {
	RetSvc  *retrieval.Service
	Metrics *metrics.Metrics
	Refs    *serverstate.Ref
}

// New constructs an HTTPService. RetSvc is non-nil in production —
// wired by cli/serve.go via retrieval.NewService(env.DB, env.VI,
// env.Embedder). Tests pass a freshly-constructed domain Service to
// avoid sharing state across parallel test bodies.
func New(retSvc *retrieval.Service, m *metrics.Metrics, refs *serverstate.Ref) *HTTPService {
	return &HTTPService{RetSvc: retSvc, Metrics: m, Refs: refs}
}

// Routes returns the URL → handler mapping for this service.
//
// /contradictions moved to src/internal/server/contradiction in
// PHASE 2.3 (ContradictionService extraction). It is no longer
// registered here.
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/search":        s.HandleSearch,
		"/retrieve":      s.HandleRetrieve,
		"/query":         s.HandleQuery,
		"/response":      s.HandleResponse,
		"/query/explain": s.HandleQueryExplain,
		"/provenance":    s.HandleProvenance,
	}
}

// optsFromState builds a per-request RetrieveContextOptions seeded from
// the atomic *serverstate.State. Callers add per-request fields
// (QueryText, QueryEmbedding, Ctx, MaxDepth, Explain) after this
// returns.
func (s *HTTPService) optsFromState() core.RetrieveContextOptions {
	state := s.Refs.Load()
	return core.RetrieveContextOptions{
		DepthCeiling:      state.DepthCeiling,
		MaxRetrievedNodes: state.MaxRetrievedNodes,
		RankingWeight:     state.RankingWeight,
		Reranker:          state.Reranker,
	}
}

// HandleSearch — POST /search. Embed the query, return top-K
// nearest neighbours.
func (s *HTTPService) HandleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.SearchRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.Query == "" {
		httputil.WriteError(w, http.StatusBadRequest, "query required")
		return
	}
	results, err := s.RetSvc.Search(r.Context(), req.Query, req.TopK)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncSearch()
	httputil.WriteJSON(w, http.StatusOK, results)
}

// HandleRetrieve — POST /retrieve. Graph-walk from explicit seed IDs
// (no embedding step — caller already chose the seeds).
func (s *HTTPService) HandleRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.RetrieveRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if len(req.SeedIDs) == 0 {
		// Defense-in-depth duplicate of domain validation. Pre-PHASE-2.2
		// HTTP shell checked this inline so /retrieve clients see 400 on
		// empty seeds without ever crossing into the domain.
		httputil.WriteError(w, http.StatusBadRequest, "seed_ids required")
		return
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = retrieval.DefaultRetrieveMaxDepth
	}
	opts := s.optsFromState()
	opts.MaxDepth = req.MaxDepth
	opts.Ctx = r.Context()
	result, err := s.RetSvc.Retrieve(r.Context(), req.SeedIDs, opts)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncRetrieve()
	httputil.WriteJSON(w, http.StatusOK, result)
}

// HandleQuery — POST /query. Embed → vector search → graph walk →
// Markdown context blob.
func (s *HTTPService) HandleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.SearchRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.Query == "" {
		httputil.WriteError(w, http.StatusBadRequest, "query required")
		return
	}
	opts := s.optsFromState()
	opts.QueryText = req.Query
	markdown, err := s.RetSvc.Query(r.Context(), req.Query, req.TopK, opts)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncQuery()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"context": markdown})
}

// HandleResponse — POST /response. Generate a natural-language answer
// from the retrieved graph context.
func (s *HTTPService) HandleResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req struct {
		Query    string `json:"query"`
		MaxDepth int    `json:"max_depth,omitempty"`
	}
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.Query == "" {
		httputil.WriteError(w, http.StatusUnprocessableEntity, "query is required")
		return
	}
	opts := s.optsFromState()
	if req.MaxDepth > 0 {
		opts.MaxDepth = req.MaxDepth
	}
	out, err := s.RetSvc.Response(r.Context(), req.Query, opts)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, "response generation failed: "+err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"response": out})
}

// HandleQueryExplain — POST /query/explain. Same pipeline as /query
// but with Explain=true so the returned RetrievalResult carries the
// per-hop ranking reasoning.
func (s *HTTPService) HandleQueryExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.SearchRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	opts := s.optsFromState()
	opts.QueryText = req.Query
	result, err := s.RetSvc.Explain(r.Context(), req.Query, req.TopK, opts)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncQuery()
	httputil.WriteJSON(w, http.StatusOK, result)
}

// HandleProvenance — GET /provenance[?conversation=X&message=Y&source=Z&limit=N].
// 400 is intentional for empty-triple + missing filters (matches
// pre-PHASE-2.2 contract — slightly counterintuitive but preserves
// clients).
func (s *HTTPService) HandleProvenance(w http.ResponseWriter, r *http.Request) {
	convID := r.URL.Query().Get("conversation_id")
	msgID := r.URL.Query().Get("message_id")
	source := r.URL.Query().Get("source")
	limit := httputil.ParseIntParam(r, "limit", 50)
	entities, err := s.RetSvc.Provenance(r.Context(), convID, msgID, source, limit)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, entities)
}

// HandleContradictions moved to src/internal/server/contradiction in
// PHASE 2.3 (ContradictionService extraction). The route is no longer
// registered in this HTTPService.
