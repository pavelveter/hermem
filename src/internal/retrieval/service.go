// Package retrieval hosts the read-side domain logic — graph-walk
// retrieval from seed IDs, vector search, query → markdown formatting,
// response generation, explanation, and provenance lookup.
package retrieval

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Default topK/maxDepth/limit values. Defaults are kept BELOW or equal to
// vector.MaxResultsCap so that any future bump of the cap doesn't silently
// escalate the per-consumer default. The `min()` wrapper makes the
// dependency explicit; today both resolve to 5 and 3 respectively (since
// 5 < 500 and 3 < 500). See retrieval docs + ADR-020 for the recall/latency
// rationale behind the specific small numbers.
const (
	DefaultSearchTopK       = min(5, vector.MaxResultsCap) // ADR-020: balances recall vs latency for typical queries
	DefaultQueryTopK        = min(3, vector.MaxResultsCap) // ADR-020: focused results for LLM context window
	DefaultRetrieveMaxDepth = 2                            // ADR-020: two-hop captures friends-of-friends
	DefaultProvenanceLimit  = 50                           // ADR-020: caps audit trail response size
)

// Service is the transport-agnostic read-side domain API.
type Service struct {
	db       *sql.DB
	vi       core.VectorIndex
	embedder core.Embedder
}

// New constructs a Service. embedder is required (Search/Query/
// Response/Explain all reach for it); pass a no-op stub in tests that
// don't exercise the embedding path.
func New(db *sql.DB, vi core.VectorIndex, embedder core.Embedder) *Service {
	return &Service{db: db, vi: vi, embedder: embedder}
}

// Search embeds the query and returns top-K nearest neighbours.
func (s *Service) Search(ctx context.Context, query string, topK int) ([]core.SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("search: query required")
	}
	if topK <= 0 {
		topK = DefaultSearchTopK
	}
	embedding, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	results, err := vector.SearchByVector(ctx, s.db, s.vi, embedding, topK)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return results, nil
}

// Retrieve runs the graph-walk from seed IDs and returns the ranked
// RetrievalResult. Empty seed list is rejected.
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

// RetrieveContext satisfies core.Retriever by delegating to the package-level
// RetrieveContext function.
func (s *Service) RetrieveContext(ctx context.Context, seedIDs []string, opts core.RetrieveContextOptions) (*core.RetrievalResult, error) {
	return s.Retrieve(ctx, seedIDs, opts)
}

// MultiHopRetrieveContext satisfies core.Retriever by delegating to the
// package-level MultiHopRetrieveContext function.
func (s *Service) MultiHopRetrieveContext(ctx context.Context, vi core.VectorIndex, embedder core.Embedder, seedIDs []string, opts core.RetrieveContextOptions) (*core.RetrievalResult, error) {
	if opts.Ctx == nil {
		opts.Ctx = ctx
	}
	return MultiHopRetrieveContext(s.db, vi, embedder, seedIDs, opts)
}

// ResolveSeeds embeds the query and returns top-K nearest entity IDs
// via vector search. Errors propagate so callers can surface them;
// an empty result is valid (degrades to exact-match graph walk).
func (s *Service) ResolveSeeds(ctx context.Context, query string, topK int) ([]string, error) {
	if query == "" {
		return nil, fmt.Errorf("resolve seeds: query required")
	}
	if topK <= 0 {
		topK = DefaultQueryTopK
	}
	embedding, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("resolve seeds: %w", err)
	}
	results, _ := vector.SearchByVector(ctx, s.db, s.vi, embedding, topK)
	seedIDs := make([]string, 0, len(results))
	for _, r := range results {
		seedIDs = append(seedIDs, r.Entity.ID)
	}
	return seedIDs, nil
}

// Query embeds → runs vector search → uses top-K as seeds → graph
// walks → returns a Markdown context blob.
func (s *Service) Query(ctx context.Context, query string, topK int, opts core.RetrieveContextOptions) (string, error) {
	result, err := s.QueryResult(ctx, query, topK, opts)
	if err != nil {
		return "", err
	}
	return (&MarkdownRenderer{}).Render(result), nil
}

// QueryResult embeds → runs vector search → uses top-K as seeds → graph
// walks → returns the RetrievalResult data model. Callers can render
// using any Renderer (MarkdownRenderer, PlainTextRenderer, JSONRenderer, etc.)
// or use the convenience Query() method for markdown output.
// If opts.TopK is set, it takes precedence over the topK parameter.
func (s *Service) QueryResult(ctx context.Context, query string, topK int, opts core.RetrieveContextOptions) (*core.RetrievalResult, error) {
	if opts.TopK > 0 {
		topK = opts.TopK
	}
	seedIDs, err := s.ResolveSeeds(ctx, query, topK)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	if opts.Ctx == nil {
		opts.Ctx = ctx
	}
	result, err := RetrieveContext(s.db, seedIDs, opts)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	return result, nil
}

// Response delegates to pkgretrieval.GenerateResponse — the LLM-rendered
// natural-language answer that uses the retrieved graph context as
// evidence.
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
// Embed errors are swallowed (degrades to empty pipeline).
func (s *Service) Explain(ctx context.Context, query string, topK int, opts core.RetrieveContextOptions) (*core.RetrievalResult, error) {
	if topK <= 0 {
		topK = DefaultQueryTopK
	}
	embedding, _ := s.embedder.Embed(ctx, query)
	results, _ := vector.SearchByVector(ctx, s.db, s.vi, embedding, topK)
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

// ExplainNode returns a ScoreBreakdown for a single entity by ID.
// When queryText is non-empty, it embeds the query and includes vector
// similarity in the breakdown. Otherwise VectorScore is 0.
//
// Computes recency (from UpdatedAt), temporal decay (from CreatedAt),
// and centrality (from edge degree). PathScore and DepthPenalty are 0
// (no graph-walk context). Returns nil if the entity is not found.
func (s *Service) ExplainNode(ctx context.Context, id, queryText string) (*core.ScoreBreakdown, error) {
	if id == "" {
		return nil, fmt.Errorf("explain node: id required")
	}

	entity, embedding, _, err := store.GetExplainEntity(s.db, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("explain node: %w", err)
	}

	// Use zero-value weights so callers see the raw feature values.
	w := core.RankingWeight{}.WithDefaults()

	var queryEmbedding []float32
	var queryNorm float32
	if queryText != "" {
		if emb, err := s.embedder.Embed(ctx, queryText); err == nil && len(emb) > 0 {
			queryEmbedding = emb
			queryNorm = vector.VectorNorm(emb)
		}
	}

	node := core.GraphNode{
		Entity:     entity,
		PathWeight: 0,
	}

	comps := ComputeScoreComponents(node, embedding, queryEmbedding, queryNorm, w)
	return BuildScoreBreakdown(comps, w), nil
}

// Provenance queries entities by provenance triple (conversation_id,
// message_id, source). Empty triple + non-positive limit returns a
// reasonable default.
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
