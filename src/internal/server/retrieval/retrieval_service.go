// Package retrieval exposes retrieval.Service over HTTP. One §3.2
// DELIBERATE EXCEPTION at /provenance (bespoke 400-on-all mapping
// preserves pre-§3.2 client wire bytes; see retrieval_service.go
// HandleProvenance for the rationale).
//
// §3.2 — embeds shared.BaseHTTPService. Five of six handlers route
// through s.Wrap so the IncErr + WriteError(500,...) boilerplate is
// collapsed. /provenance is the deliberate exception: its pre-§3.2
// contract mapped ALL domain errors to 400 (a quirk to preserve
// clients — domain errors actually mean "your filter params are
// incomplete"), so HandleProvenance stays inline and Registers as a
// raw handler without s.Wrap.
package retrieval

import (
	"fmt"
	"net/http"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/server/shared"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService is the transport shell for the read-side domain.
//
// Holds the domain Service (Svc) + the embedded BaseHTTPService
// (Metrics, Refs promoted). Field-promotion keeps `s.Metrics.*` and
// `s.Refs.Load()` working unchanged in handler code.
type HTTPService struct {
	Svc *retrieval.Service
	shared.BaseHTTPService
}

// New constructs an HTTPService. Svc is non-nil in production —
// wired by cli/serve.go via retrieval.New(env.DB, env.VI,
// env.Embedder). Tests pass a freshly-constructed domain Service to
// avoid sharing state across parallel test bodies.
func New(svc *retrieval.Service, m *metrics.Metrics, refs *serverstate.Ref) *HTTPService {
	return &HTTPService{
		Svc: svc,
		BaseHTTPService: shared.BaseHTTPService{
			Metrics: m,
			Refs:    refs,
		},
	}
}

// Routes returns the URL → handler mapping for this service.
//
// /contradictions moved to src/internal/server/contradiction in
// PHASE 2.3. §3.2 wraps 5/6 handlers via s.Wrap. /provenance is the
// deliberate inline exception (see HandleProvenance for rationale).
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/search":        s.Wrap(s.HandleSearch),
		"/retrieve":      s.Wrap(s.HandleRetrieve),
		"/query":         s.Wrap(s.HandleQuery),
		"/query/temporal": s.Wrap(s.HandleQueryTemporal),
		"/response":      s.Wrap(s.HandleResponse),
		"/query/explain": s.Wrap(s.HandleQueryExplain),
		"/provenance":    s.HandleProvenance, // NOT wrapped — bespoke 400 contract
	}
}

// optsFromState — unchanged from pre-§3.2 (helper, not a handler).
func (s *HTTPService) optsFromState() core.RetrieveContextOptions {
	state := s.Refs.Load()
	return core.RetrieveContextOptions{
		DepthCeiling:      state.DepthCeiling,
		MaxRetrievedNodes: state.MaxRetrievedNodes,
		TokenBudget:       state.TokenBudget,
		RankingWeight:     state.RankingWeight,
		Reranker:          state.Reranker,
	}
}

// HandleSearch — POST /search. Embed the query, return top-K
// nearest neighbours.
//
// §3.2 — error-returning handler.
func (s *HTTPService) HandleSearch(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	req, err := httputil.DecodeJSON[core.SearchRequest](w, r)
	if err != nil {
		return err
	}
	if req.Query == "" {
		httputil.WriteErrorWithCode(w, http.StatusUnprocessableEntity, &core.DomainError{Code: core.CodeInvalidInput, Message: "query required", Field: "query"})
		return nil
	}
	results, err := s.Svc.Search(r.Context(), req.Query, req.TopK)
	if err != nil {
		return err
	}
	s.Metrics.IncSearch()
	httputil.WriteJSON(w, http.StatusOK, results)
	return nil
}

// HandleRetrieve — POST /retrieve.
//
// §3.2 — error-returning handler.
func (s *HTTPService) HandleRetrieve(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	req, err := httputil.DecodeJSON[core.RetrieveRequest](w, r)
	if err != nil {
		return err
	}
	if len(req.SeedIDs) == 0 {
		httputil.WriteErrorWithCode(w, http.StatusUnprocessableEntity, &core.DomainError{Code: core.CodeInvalidInput, Message: "seed_ids required", Field: "seed_ids"})
		return nil
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = retrieval.DefaultRetrieveMaxDepth
	}
	opts := s.optsFromState()
	opts.MaxDepth = req.MaxDepth
	opts.Ctx = r.Context()
	result, err := s.Svc.Retrieve(r.Context(), req.SeedIDs, opts)
	if err != nil {
		return err
	}
	s.Metrics.IncRetrieve()
	httputil.WriteJSON(w, http.StatusOK, result)
	return nil
}

// HandleQuery — POST /query.
//
// §3.2 — error-returning handler.
func (s *HTTPService) HandleQuery(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	req, err := httputil.DecodeJSON[core.SearchRequest](w, r)
	if err != nil {
		return err
	}
	if req.Query == "" {
		httputil.WriteErrorWithCode(w, http.StatusUnprocessableEntity, &core.DomainError{Code: core.CodeInvalidInput, Message: "query required", Field: "query"})
		return nil
	}
	opts := s.optsFromState()
	opts.QueryText = req.Query
	markdown, err := s.Svc.Query(r.Context(), req.Query, req.TopK, opts)
	if err != nil {
		return err
	}
	s.Metrics.IncQuery()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"context": markdown})
	return nil
}

// HandleQueryTemporal — POST /query/temporal.
//
// Full retrieval pipeline filtered by time range (RFC3339).
// §3.2 — error-returning handler.
func (s *HTTPService) HandleQueryTemporal(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	req, err := httputil.DecodeJSON[core.TemporalQueryRequest](w, r)
	if err != nil {
		return err
	}
	if req.Query == "" {
		httputil.WriteErrorWithCode(w, http.StatusUnprocessableEntity, &core.DomainError{Code: core.CodeInvalidInput, Message: "query required", Field: "query"})
		return nil
	}
	opts := s.optsFromState()
	opts.QueryText = req.Query
	if req.TimeFrom != "" {
		t, err := time.Parse(time.RFC3339, req.TimeFrom)
		if err != nil {
			httputil.WriteErrorWithCode(w, http.StatusUnprocessableEntity, &core.DomainError{Code: core.CodeInvalidInput, Message: "invalid time_from: must be RFC3339", Field: "time_from"})
			return nil
		}
		opts.TimeFrom = t
	}
	if req.TimeTo != "" {
		t, err := time.Parse(time.RFC3339, req.TimeTo)
		if err != nil {
			httputil.WriteErrorWithCode(w, http.StatusUnprocessableEntity, &core.DomainError{Code: core.CodeInvalidInput, Message: "invalid time_to: must be RFC3339", Field: "time_to"})
			return nil
		}
		opts.TimeTo = t
	}
	markdown, err := s.Svc.Query(r.Context(), req.Query, req.TopK, opts)
	if err != nil {
		return err
	}
	s.Metrics.IncQuery()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"context": markdown})
	return nil
}

// HandleResponse — POST /response.
//
// §3.2 — error-returning handler. The "response generation failed: "
// message prefix is preserved by wrapping the domain error before
// returning; mapStatus falls through to 500 default.
func (s *HTTPService) HandleResponse(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	req, err := httputil.DecodeJSON[struct {
		Query    string `json:"query"`
		MaxDepth int    `json:"max_depth,omitempty"`
	}](w, r)
	if err != nil {
		return err
	}
	if req.Query == "" {
		httputil.WriteError(w, http.StatusUnprocessableEntity, "query is required")
		return nil
	}
	opts := s.optsFromState()
	if req.MaxDepth > 0 {
		opts.MaxDepth = req.MaxDepth
	}
	out, err := s.Svc.Response(r.Context(), req.Query, opts)
	if err != nil {
		// Wrap with the pre-§3.2 "response generation failed: " prefix
		// so the wire contract (message text + status 500) survives.
		// fmt.Errorf with %w preserves errors.Is / errors.As unwrap
		// semantics — mapStatus walks the chain if needed.
		return fmt.Errorf("response generation failed: %w", err)
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"response": out})
	return nil
}

// HandleQueryExplain — POST /query/explain.
//
// §3.2 — error-returning handler.
func (s *HTTPService) HandleQueryExplain(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	req, err := httputil.DecodeJSON[core.SearchRequest](w, r)
	if err != nil {
		return err
	}
	opts := s.optsFromState()
	opts.QueryText = req.Query
	result, err := s.Svc.Explain(r.Context(), req.Query, req.TopK, opts)
	if err != nil {
		return err
	}
	s.Metrics.IncQuery()
	httputil.WriteJSON(w, http.StatusOK, result)
	return nil
}

// HandleProvenance — GET /provenance[?conversation=X&message=Y&source=Z&limit=N].
//
// §3.2 DELIBERATE EXCEPTION to s.Wrap. Pre-§3.2 contract maps ALL
// domain errors to 400 (slightly counterintuitive — domain errors
// from /provenance actually mean "your filter triple is incomplete" —
// but preserves clients). This contract is NOT preserved by
// server.mapStatus' default 500 mapping, so HandleProvenance stays
// inline and Routes() registers it WITHOUT s.Wrap. Wire bytes
// identical to pre-§3.2.
func (s *HTTPService) HandleProvenance(w http.ResponseWriter, r *http.Request) {
	convID := r.URL.Query().Get("conversation_id")
	msgID := r.URL.Query().Get("message_id")
	source := r.URL.Query().Get("source")
	limit := httputil.ParseIntParam(r, "limit", 50)
	entities, err := s.Svc.Provenance(r.Context(), convID, msgID, source, limit)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, entities)
}

// HandleContradictions moved to src/internal/server/contradiction in
// PHASE 2.3.
