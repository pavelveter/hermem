// Package retrieval hosts the read-side domain logic — graph-walk
// retrieval from seed IDs, vector search, query → markdown formatting,
// response generation, explanation, and provenance lookup.
//
// Before PHASE 2.2 callers reached the pkgretrieval functions directly
// (RetrieveContext, GenerateResponse, FormatContextMarkdown, …). Those
// free-function entry points still exist and are the workhorses —
// store/walk/scoring/response internals haven't moved. What PHASE 2.2
// adds is a thin Service struct so HTTP handlers and CLI subcommands
// share a uniform pointer-based API without changing domain semantics.
//
// Construction is cheap (three pointer fields) so callers may instantiate
// fresh per request, but production HTTP boot (cli/serve.go) typically
// holds one Service for the daemon's lifetime and threads it as
// RetSvc into the server/retrieval HTTP shell.
package retrieval

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Default topK/maxDepth/limit values chosen to match the pre-PHASE-2.2
// HTTP shell defaults. Constants are exported so tests can assert the
// exact defaults without hard-coding magic numbers.
const (
	DefaultSearchTopK       = 5
	DefaultQueryTopK        = 3
	DefaultRetrieveMaxDepth = 2
	DefaultProvenanceLimit  = 50
)

// Service is the transport-agnostic read-side domain API.
//
// State-load (DepthCeiling / MaxRetrievedNodes / RankingWeight / Reranker
// / etc.) is the caller's responsibility — Service methods accept a
// fully-built core.RetrieveContextOptions that the caller populates
// from its state source. This mirrors MemoryService's pattern of taking
// `schema core.SchemaConfig` per call rather than holding a Ref pointer:
// keeps the domain tier pure, lets both HTTP and CLI build opts from
// their respective state sources (atomic *serverstate.Ref at request
// time vs static *config.Config at startup).
type Service struct {
	db       *sql.DB
	vi       core.VectorIndex
	embedder core.Embedder
}

// NewService constructs a Service. embedder is required (Search/Query/
// Response/Explain all reach for it); pass a no-op stub in tests that
// don't exercise the embedding path.
func NewService(db *sql.DB, vi core.VectorIndex, embedder core.Embedder) *Service {
	return &Service{db: db, vi: vi, embedder: embedder}
}

// Search embeds the query and returns top-K nearest neighbours.
//
// Both embedding and vector-search errors propagate up so callers can
// surface them as 500. Pre-PHASE-2.2 both HTTP and CLI did the same.
func (s *Service) Search(ctx context.Context, query string, topK int) ([]core.SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("search: query required")
	}
	if topK <= 0 {
		topK = DefaultSearchTopK
	}
	embedding, err := s.embedder.Embed(ctx, query) //nolint:errcheck // best-effort: zero-vector query (Service.Query) reduces to nil; ctx.Err surfaces cancellation upstream
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	results, err := vector.SearchByVector(s.db, s.vi, embedding, topK)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return results, nil
}

// Retrieve runs the graph-walk from seed IDs and returns the ranked
// RetrievalResult. Empty seed list is rejected — the caller's
// pre-validation should already reject this but defense-in-depth.
//
// opts.Ctx is filled from ctx if nil so callers that build opts
// without explicitly threading ctx (CLI) still get cancellation
// propagation through the retrieval path.
func (s *Service) Retrieve(ctx context.Context, seedIDs []string, opts core.RetrieveContextOptions) (*core.RetrievalResult, error) {
	if len(seedIDs) == 0 {
		return nil, fmt.Errorf("retrieve: seed_ids required")
	}
	if opts.Ctx == nil {
		opts.Ctx = ctx
	}
	result, err := RetrieveContext(s.db, seedIDs, opts)
	if err != nil {
		return nil, fmt.Errorf("retrieve: %w", err)
	}
	return result, nil
}

// Query embeds → runs vector search → uses top-K as seeds → graph
// walks → returns a Markdown context blob (pkgretrieval.FormatContextMarkdown).
//
// Pre-PHASE-2.2 HTTP HandleQuery propagated embed errors but swallowed
// vector-search errors (graceful degradation: empty seed pool → empty
// graph result). That shape is preserved here — embed propagates,
// search swallows. CLI used to swallow both; after the refactor, CLI
// gets the same HTTP shape (errors surface as 500 on embed failures,
// empty result on search failures).
func (s *Service) Query(ctx context.Context, query string, topK int, opts core.RetrieveContextOptions) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query: query required")
	}
	if topK <= 0 {
		topK = DefaultQueryTopK
	}
	embedding, err := s.embedder.Embed(ctx, query) //nolint:errcheck // best-effort: zero-vector query (Service.Query) reduces to nil; ctx.Err surfaces cancellation upstream
	if err != nil {
		return "", fmt.Errorf("query: %w", err)
	}
	results, _ := vector.SearchByVector(s.db, s.vi, embedding, topK) //nolint:errcheck // best-effort: empty seed pool degrades to graph-walk-exact-match
	seedIDs := make([]string, 0, len(results))
	for _, r := range results {
		seedIDs = append(seedIDs, r.Entity.ID)
	}
	if opts.Ctx == nil {
		opts.Ctx = ctx
	}
	result, err := RetrieveContext(s.db, seedIDs, opts)
	if err != nil {
		return "", fmt.Errorf("query: %w", err)
	}
	return FormatContextMarkdown(result), nil
}

// Response delegates to pkgretrieval.GenerateResponse — the LLM-rendered
// natural-language answer that uses the retrieved graph context as
// evidence. opts.MaxDepth set by caller; empty query rejected here as
// defense-in-depth (callers should pre-validate).
func (s *Service) Response(ctx context.Context, query string, opts core.RetrieveContextOptions) (string, error) {
	if query == "" {
		return "", fmt.Errorf("response: query required")
	}
	out, err := GenerateResponse(ctx, s.db, s.vi, s.embedder, opts, query)
	if err != nil {
		return "", fmt.Errorf("response: %w", err)
	}
	return out, nil
}

// Explain runs Query-equivalent pipeline with Explain=true so the
// returned RetrievalResult carries the per-hop ranking reasoning
// (RankingScore / PathWeight / ParentID populated for each node).
//
// Pre-PHASE-2.2 HTTP HandleQueryExplain and CLI newExplainCmd swallowed
// both Embed and Search errors (graceful degradation: a broken
// embedder or empty vector index produced an empty graph result
// instead of a 500). PHASE 2.2 preserves that shape — empty pipeline,
// not loud failure. Query diverges intentionally (it propagates
// embed errors per its pre-PHASE-2.2 HTTP contract).
func (s *Service) Explain(ctx context.Context, query string, topK int, opts core.RetrieveContextOptions) (*core.RetrievalResult, error) {
	if topK <= 0 {
		topK = DefaultQueryTopK
	}
	embedding, _ := s.embedder.Embed(ctx, query) //nolint:errcheck // best-effort: zero-vector query (Service.Query) reduces to nil; ctx.Err surfaces cancellation upstream
	results, _ := vector.SearchByVector(s.db, s.vi, embedding, topK) //nolint:errcheck // best-effort: empty seed pool degrades to graph-walk-exact-match
	seedIDs := make([]string, 0, len(results))
	for _, r := range results {
		seedIDs = append(seedIDs, r.Entity.ID)
	}
	opts.Explain = true
	if opts.Ctx == nil {
		opts.Ctx = ctx
	}
	result, err := RetrieveContext(s.db, seedIDs, opts)
	if err != nil {
		return nil, fmt.Errorf("explain: %w", err)
	}
	return result, nil
}

// Provenance queries entities by provenance triple (conversation_id,
// message_id, source). Empty triple + non-positive limit returns a
// reasonable default. The HTTP shell maps the domain error to 400
// (pre-PHASE-2.2 contract) — this Service propagates the error
// verbatim so callers retain control of the envelope.
func (s *Service) Provenance(ctx context.Context, convID, msgID, source string, limit int) ([]core.Entity, error) {
	if limit <= 0 {
		limit = DefaultProvenanceLimit
	}
	entities, err := store.GetEntitiesByProvenance(s.db, convID, msgID, source, limit)
	if err != nil {
		return nil, fmt.Errorf("provenance: %w", err)
	}
	return entities, nil
}
