// Package retrieval hosts the read-only HTTP service: search, retrieve, query,
// response, query_explain, provenance, contradictions.
//
// All handlers in this service read RankingWeight/Reranker/DepthCeiling from
// the atomic *serverstate.Ref on every request — never from any shared mutable
// struct field — so a SIGHUP-driven state swap is safe to run concurrently
// with in-flight handlers.
package retrieval

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	pkgretrieval "github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Service handles read-only endpoints: search, retrieve, query, response,
// query_explain, provenance, contradictions.
type Service struct {
	DB       *sql.DB
	VI       core.VectorIndex
	Embedder core.Embedder
	Refs     *serverstate.Ref
}

// New constructs a Service.
func New(db *sql.DB, vi core.VectorIndex, embedder core.Embedder, refs *serverstate.Ref) *Service {
	return &Service{DB: db, VI: vi, Embedder: embedder, Refs: refs}
}

// Routes returns the URL → handler mapping for this service.
func (s *Service) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/search":         s.HandleSearch,
		"/retrieve":       s.HandleRetrieve,
		"/query":          s.HandleQuery,
		"/response":       s.HandleResponse,
		"/query/explain":  s.HandleQueryExplain,
		"/provenance":     s.HandleProvenance,
		"/contradictions": s.HandleContradictions,
	}
}

// optsFromState builds a per-request RetrieveContextOptions seeded from the
// atomic *serverstate.State. Callers add per-request fields (QueryText,
// QueryEmbedding, Ctx, MaxDepth, Explain) after this returns.
func (s *Service) optsFromState() core.RetrieveContextOptions {
	state := s.Refs.Load()
	return core.RetrieveContextOptions{
		DepthCeiling:      state.DepthCeiling,
		MaxRetrievedNodes: state.MaxRetrievedNodes,
		RankingWeight:     state.RankingWeight,
		Reranker:          state.Reranker,
	}
}

func (s *Service) HandleSearch(w http.ResponseWriter, r *http.Request) {
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
	if req.TopK <= 0 {
		req.TopK = 5
	}
	embedding, err := s.Embedder.Embed(r.Context(), req.Query)
	if err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("embed failed: %v", err))
		return
	}
	results, err := vector.SearchByVector(s.DB, s.VI, embedding, req.TopK)
	if err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncSearch()
	httputil.WriteJSON(w, http.StatusOK, results)
}

func (s *Service) HandleRetrieve(w http.ResponseWriter, r *http.Request) {
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
		httputil.WriteError(w, http.StatusBadRequest, "seed_ids required")
		return
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = 2
	}
	opts := s.optsFromState()
	opts.MaxDepth = req.MaxDepth
	opts.Ctx = r.Context()
	result, err := pkgretrieval.RetrieveContext(s.DB, req.SeedIDs, opts)
	if err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncRetrieve()
	httputil.WriteJSON(w, http.StatusOK, result)
}

func (s *Service) HandleQuery(w http.ResponseWriter, r *http.Request) {
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
	if req.TopK <= 0 {
		req.TopK = 3
	}
	embedding, err := s.Embedder.Embed(r.Context(), req.Query)
	if err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	results, _ := vector.SearchByVector(s.DB, s.VI, embedding, req.TopK)
	seedIDs := make([]string, 0, len(results))
	for _, res := range results {
		seedIDs = append(seedIDs, res.Entity.ID)
	}
	opts := s.optsFromState()
	opts.QueryEmbedding = embedding
	opts.QueryText = req.Query
	opts.Ctx = r.Context()
	ctxResult, err := pkgretrieval.RetrieveContext(s.DB, seedIDs, opts)
	if err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncQuery()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"context": pkgretrieval.FormatContextMarkdown(ctxResult)})
}

func (s *Service) HandleResponse(w http.ResponseWriter, r *http.Request) {
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
	out, err := pkgretrieval.GenerateResponse(r.Context(), s.DB, s.VI, s.Embedder, opts, req.Query)
	if err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, "response generation failed: "+err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"response": out})
}

func (s *Service) HandleQueryExplain(w http.ResponseWriter, r *http.Request) {
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
	if req.TopK <= 0 {
		req.TopK = 3
	}
	emb, _ := s.Embedder.Embed(r.Context(), req.Query)
	results, _ := vector.SearchByVector(s.DB, s.VI, emb, req.TopK)
	seedIDs := make([]string, 0, len(results))
	for _, res := range results {
		seedIDs = append(seedIDs, res.Entity.ID)
	}
	opts := s.optsFromState()
	opts.QueryEmbedding = emb
	opts.QueryText = req.Query
	opts.Ctx = r.Context()
	opts.Explain = true
	result, err := pkgretrieval.RetrieveContext(s.DB, seedIDs, opts)
	if err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncQuery()
	httputil.WriteJSON(w, http.StatusOK, result)
}

func (s *Service) HandleProvenance(w http.ResponseWriter, r *http.Request) {
	entities, err := store.GetEntitiesByProvenance(s.DB,
		r.URL.Query().Get("conversation_id"), r.URL.Query().Get("message_id"),
		r.URL.Query().Get("source"), httputil.ParseIntParam(r, "limit", 50))
	if err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, entities)
}

func (s *Service) HandleContradictions(w http.ResponseWriter, r *http.Request) {
	pairs, err := store.GetContradictions(s.DB, r.URL.Query().Get("id"))
	if err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if pairs == nil {
		pairs = []core.ContradictionPair{}
	}
	httputil.WriteJSON(w, http.StatusOK, pairs)
}
