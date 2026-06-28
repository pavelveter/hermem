package retrieval

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
)

// PipelineStage is a single stage in the retrieval pipeline.
// Each stage transforms its input into an output. Stages are
// independently testable and replaceable.
type PipelineStage interface {
	Name() string
}

// QueryStage resolves a natural-language query into seed entity IDs.
type QueryStage interface {
	PipelineStage
	ResolveSeeds(ctx context.Context, query string, topK int) ([]string, error)
}

// EmbeddingStage converts a query string into a vector embedding.
type EmbeddingStage interface {
	PipelineStage
	Embed(ctx context.Context, text string) ([]float32, error)
}

// CandidateRetrievalStage fetches raw graph nodes from seed IDs.
type CandidateRetrievalStage interface {
	PipelineStage
	Expand(ctx context.Context, db *sql.DB, seedIDs []string, opts core.RetrieveContextOptions, effDepth int) ([]scannedNode, error)
}

// RankingStage scores and sorts candidates.
type RankingStage interface {
	PipelineStage
	Rank(items []scannedNode, opts core.RetrieveContextOptions, w core.RankingWeight, scorer core.CompositeScorer) ([]rankedNode, []core.GraphNode)
}

// ContextAssemblyStage buckets ranked nodes into the RetrievalResult.
type ContextAssemblyStage interface {
	PipelineStage
	Assemble(ranked []rankedNode, seeds []core.GraphNode, w core.RankingWeight, explain bool) *core.RetrievalResult
}

// RenderingStage converts a RetrievalResult into a string representation.
// This is a subset of the existing Renderer interface — any Renderer
// (MarkdownRenderer, PlainTextRenderer, JSONRenderer) satisfies it.
type RenderingStage interface {
	Render(result *core.RetrievalResult) string
}

// Pipeline is the explicit, stage-based retrieval pipeline.
// Each stage is a pluggable implementation; default implementations
// delegate to the existing package-level functions.
type Pipeline struct {
	expand   CandidateRetrievalStage
	rank     RankingStage
	assemble ContextAssemblyStage
	render   RenderingStage
}

// NewPipeline creates a Pipeline with default stage implementations.
func NewPipeline() *Pipeline {
	return &Pipeline{
		expand:   &defaultExpandStage{},
		rank:     &defaultRankStage{},
		assemble: &defaultAssemblyStage{},
		render:   &MarkdownRenderer{},
	}
}

// SetExpand replaces the graph expansion stage.
func (p *Pipeline) SetExpand(s CandidateRetrievalStage) { p.expand = s }

// SetRank replaces the ranking stage.
func (p *Pipeline) SetRank(s RankingStage) { p.rank = s }

// SetAssembly replaces the context assembly stage.
func (p *Pipeline) SetAssembly(s ContextAssemblyStage) { p.assemble = s }

// SetRender replaces the rendering stage.
func (p *Pipeline) SetRender(s RenderingStage) { p.render = s }

// Run executes the full pipeline: expand → rank → assemble → render.
func (p *Pipeline) Run(db *sql.DB, seedIDs []string, opts core.RetrieveContextOptions, query string) (*core.RetrievalResult, string, error) {
	if len(seedIDs) == 0 {
		return &core.RetrievalResult{}, "", nil
	}
	effDepth := effectiveDepth(opts)
	w := opts.RankingWeight.WithDefaults()
	scorer := opts.CompositeScorer
	if scorer == nil {
		scorer = defaultCompositeScorer(w)
	}

	nodes, err := p.expand.Expand(context.Background(), db, seedIDs, opts, effDepth)
	if err != nil {
		return nil, "", fmt.Errorf("pipeline expand: %w", err)
	}

	ranked, seeds := p.rank.Rank(nodes, opts, w, scorer)
	sortByScoreDesc(ranked)

	result := p.assemble.Assemble(ranked, seeds, w, opts.Explain)

	if opts.Reranker != nil {
		if err := applyReranker(result, opts.Reranker, opts.Ctx, opts.QueryText); err != nil {
			return nil, "", fmt.Errorf("pipeline rerank: %w", err)
		}
	}

	rendered := p.render.Render(result)
	return result, rendered, nil
}

// --- Default stage implementations ---

type defaultExpandStage struct{}

func (s *defaultExpandStage) Name() string { return "expand_graph" }
func (s *defaultExpandStage) Expand(_ context.Context, db *sql.DB, seedIDs []string, opts core.RetrieveContextOptions, effDepth int) ([]scannedNode, error) {
	return expandGraph(db, seedIDs, opts, effDepth)
}

type defaultRankStage struct{}

func (s *defaultRankStage) Name() string { return "score_and_rank" }
func (s *defaultRankStage) Rank(items []scannedNode, opts core.RetrieveContextOptions, w core.RankingWeight, scorer core.CompositeScorer) ([]rankedNode, []core.GraphNode) {
	return scoreAndRank(items, opts, w, scorer)
}

type defaultAssemblyStage struct{}

func (s *defaultAssemblyStage) Name() string { return "bucketize" }
func (s *defaultAssemblyStage) Assemble(ranked []rankedNode, seeds []core.GraphNode, w core.RankingWeight, explain bool) *core.RetrievalResult {
	return bucketize(ranked, seeds, w, explain)
}
