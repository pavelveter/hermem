package retrieval

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// RetrieveContext performs a recursive CTE graph walk from seed IDs and returns re-ranked results.
//
// Pipeline (four stages; each is a named helper below):
//
//  1. expandGraph       — SQL CTE walk; returns raw []GraphNode rows.
//  2. scoreAndRank      — applies the composite scorer (or ScoreComponents
//     on the Explain path) per row; collects depth-0
//     seeds; sorts by RankingScore DESC.
//  3. bucketize         — content-level dedup + per-category fan-out.
//  4. logRetrievalExplanation — one structured INFO per explain call.
//
// Behavior is unchanged from the pre-refactor inline implementation; the
// stage split is for clarity and per-stage testability, not a semantic
// shift.
func RetrieveContext(db *sql.DB, seedIDs []string, opts core.RetrieveContextOptions) (*core.RetrievalResult, error) {
	if len(seedIDs) == 0 {
		return &core.RetrievalResult{}, nil
	}
	effDepth := effectiveDepth(opts)

	w := opts.RankingWeight.WithDefaults()
	scorer := opts.CompositeScorer
	if scorer == nil {
		scorer = defaultCompositeScorer(w)
	}

	nodes, err := func() ([]scannedNode, error) {
		span := startStageSpan(opts, "expand_graph")
		defer span.End()
		out, e := expandGraph(db, seedIDs, opts, effDepth)
		if e != nil {
			span.RecordError(e)
		}
		return out, e
	}()
	if err != nil {
		return nil, err
	}

	ranked, seeds := func() ([]rankedNode, []core.GraphNode) {
		span := startStageSpan(opts, "score_and_rank")
		defer span.End()
		r, s := scoreAndRank(nodes, opts, w, scorer)
		span.SetAttribute("ranked_count", len(r))
		span.SetAttribute("seed_count", len(s))
		return r, s
	}()

	func() {
		span := startStageSpan(opts, "rank_sort")
		defer span.End()
		sortByScoreDesc(ranked)
		span.SetAttribute("sorted_count", len(ranked))
	}()

	result := func() *core.RetrievalResult {
		span := startStageSpan(opts, "bucketize")
		defer span.End()
		r := bucketize(ranked, seeds, w, opts.Explain)
		span.SetAttribute("world_facts", len(r.WorldFacts))
		span.SetAttribute("opinions", len(r.Opinions))
		span.SetAttribute("experiences", len(r.Experiences))
		span.SetAttribute("observations", len(r.Observations))
		return r
	}()

	if opts.Reranker != nil {
		if err := func() error {
			span := startStageSpan(opts, "rerank")
			defer span.End()
			e := applyReranker(result, opts.Reranker, opts.Ctx, opts.QueryText)
			if e != nil {
				span.RecordError(e)
			}
			return e
		}(); err != nil {
			return nil, err
		}
	}

	if opts.Explain {
		logRetrievalExplanation(result, len(seedIDs), effDepth)
	}
	return result, nil
}

// effectiveDepth resolves the requested MaxDepth against the
// DepthCeiling clamp, defaulting to 2 when unset. Pulled out so the
// RetrieveContext orchestrator stays focused on pipeline composition.
func effectiveDepth(opts core.RetrieveContextOptions) int {
	d := opts.MaxDepth
	if d <= 0 {
		d = 2
	}
	if opts.DepthCeiling > 0 && d > opts.DepthCeiling {
		d = opts.DepthCeiling
	}
	return d
}

// scoreAndRank — stage 2. Walks the expanded nodes once, applies the
// composite scorer, collects depth-0 entries into seeds, and returns
// the ranked slice (unsorted — caller invokes sortByScoreDesc).
//
// Explain=true funnels through ComputeScoreComponents so sim / recency /
// temporal / centrality / path are extracted exactly once and the
// breakdown derives from the same intermediates as the final score.
func scoreAndRank(items []scannedNode, opts core.RetrieveContextOptions, w core.RankingWeight, scorer core.CompositeScorer) ([]rankedNode, []core.GraphNode) {
	queryNorm := vector.VectorNorm(opts.QueryEmbedding)
	ranked := make([]rankedNode, 0, len(items))
	var seeds []core.GraphNode
	for _, it := range items {
		node, nodeVec := it.node, it.vec
		var (
			score     float32
			comps     ScoreComponents
			haveComps bool
		)
		if opts.Explain {
			comps = ComputeScoreComponents(node, nodeVec, opts.QueryEmbedding, queryNorm, w)
			score = comps.Final(w)
			haveComps = true
		} else {
			score = scorer(node, nodeVec, opts.QueryEmbedding, queryNorm)
		}
		node.RankingScore = score
		rn := rankedNode{node: node, score: score}
		if haveComps {
			rn.sim = comps.Sim
			rn.recency = comps.Recency
			rn.node.ScoreBreakdown = BuildScoreBreakdown(comps, w)
		}
		ranked = append(ranked, rn)
		if node.Depth == 0 {
			// Append rn.node (not node) so any ScoreBreakdown
			// attached on the Explain path propagates into SeedNodes.
			// The two are value-type copies — modifying rn.node leaves
			// node stale.
			seeds = append(seeds, rn.node)
		}
	}
	return ranked, seeds
}

// bucketize — stage 3. Content-level dedup (first-write-wins on the
// sorted ranked slice) plus per-category fan-out into the four
// RetrievalResult buckets. Explain path also propagates the legacy
// scalar VectorScore / RecencyScore / DepthPenalty / RankingScore on
// each fact for backward compat with callers predating ScoreBreakdown.
//
// Returns the full RetrievalResult with SeedNodes already set from
// stage 2.
func bucketize(ranked []rankedNode, seeds []core.GraphNode, w core.RankingWeight, explain bool) *core.RetrievalResult {
	result := &core.RetrievalResult{
		SeedNodes:    seeds,
		WorldFacts:   []core.RetrievedFact{},
		Opinions:     []core.RetrievedFact{},
		Experiences:  []core.RetrievedFact{},
		Observations: []core.RetrievedFact{},
	}
	seenContents := make(map[string]bool)
	for _, rn := range ranked {
		if seenContents[rn.node.Entity.Content] {
			continue
		}
		seenContents[rn.node.Entity.Content] = true
		fact := core.RetrievedFact{
			Content:        rn.node.Entity.Content,
			ParentID:       rn.node.ParentID,
			RelationType:   rn.node.RelationType,
			Depth:          rn.node.Depth,
			ScoreBreakdown: rn.node.ScoreBreakdown,
		}
		if explain {
			fact.VectorScore = rn.sim
			fact.RecencyScore = rn.recency
			fact.DepthPenalty = 1 - depthDecay(rn.node.PathWeight)
			fact.RankingScore = rn.score
		}
		switch rn.node.Entity.Category {
		case "world":
			result.WorldFacts = append(result.WorldFacts, fact)
		case "opinion":
			result.Opinions = append(result.Opinions, fact)
		case "experience":
			result.Experiences = append(result.Experiences, fact)
		case "observation":
			result.Observations = append(result.Observations, fact)
		}
	}
	return result
}

// applyReranker — stage 4. Invokes the optional core.Reranker on
// each non-empty bucket and replaces the bucket contents in place.
// nil Reranker is a no-op pass-through so the pipeline composition
// stays uniform across callers. Cancellation propagates through ctx
// (opts.Ctx) so a cancelled retrieval also cancels the reranker
// round-trip.
//
// Per-bucket invocation (rather than cross-bucket) keeps each
// bucket's category semantics intact — the Reranker only re-orders
// facts within their category.
func applyReranker(result *core.RetrievalResult, r core.Reranker, ctx context.Context, query string) error {
	if result == nil || r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	buckets := []struct {
		name  string
		facts *[]core.RetrievedFact
	}{
		{"world", &result.WorldFacts},
		{"opinion", &result.Opinions},
		{"experience", &result.Experiences},
		{"observation", &result.Observations},
	}
	for _, b := range buckets {
		if len(*b.facts) == 0 {
			continue
		}
		reranked, err := r.Rerank(ctx, query, *b.facts)
		if err != nil {
			return fmt.Errorf("rerank %s: %w", b.name, err)
		}
		if reranked != nil {
			*b.facts = reranked
		}
	}
	return nil
}

// logRetrievalExplanation emits a single structured INFO log per
// retrieval call (when Explain=true) summarising the per-bucket counts
// and the score breakdown of the top-ranked entry per bucket. One log
// line per call — bounded, greppable by entity ID or FinalScore.
func logRetrievalExplanation(r *core.RetrievalResult, seedCount, depth int) {
	if r == nil {
		return
	}
	slog.Info("retrieval.explain",
		"event", "retrieval.explain",
		"seeds", seedCount,
		"depth", depth,
		"seed_nodes", len(r.SeedNodes),
		"world_facts", len(r.WorldFacts),
		"opinions", len(r.Opinions),
		"experiences", len(r.Experiences),
		"observations", len(r.Observations),
		"top_world", topBreakdownForLog(r.WorldFacts),
		"top_opinion", topBreakdownForLog(r.Opinions),
		"top_experience", topBreakdownForLog(r.Experiences),
		"top_observation", topBreakdownForLog(r.Observations),
	)
}

// topBreakdownForLog returns a compact map[string]float32 of the top
// entry's breakdown so slog emits it as flat fields. Empty bucket → nil.
func topBreakdownForLog(facts []core.RetrievedFact) map[string]float32 {
	if len(facts) == 0 {
		return nil
	}
	top := facts[0]
	if top.ScoreBreakdown == nil {
		return map[string]float32{
			"content": 0,
			"final":   top.RankingScore,
		}
	}
	return map[string]float32{
		"vector":     top.ScoreBreakdown.VectorScore,
		"recency":    top.ScoreBreakdown.RecencyScore,
		"temporal":   top.ScoreBreakdown.TemporalScore,
		"centrality": top.ScoreBreakdown.CentralityScore,
		"path":       top.ScoreBreakdown.PathScore,
		"depth_pen":  top.ScoreBreakdown.DepthPenalty,
		"final":      top.ScoreBreakdown.FinalScore,
	}
}

// MultiHopRetrieveContext expands the seed set across multiple "hops" by
// interleaving shallow graph walks with vector similarity jumps.
//
// BEHAVIOUR CHANGE: callers that don't set opts.MultiHopCount now run a
// 2-hop walk and MUST supply vi + embedder or the call errors. To recover
// the prior passthrough behaviour, pass MultiHopCount=1 explicitly.
//
// Each hop:
//
//  1. Runs a shallow RetrieveContext (MaxDepth=1) from the NEW seeds
//     discovered in the previous hop — keeps per-hop work bounded.
//  2. Selects the top-K facts by ranking score (deterministic; tie-broken
//     by content string).
//  3. Embeds their content strings via the supplied Embedder.
//  4. Uses VectorIndex to find topologically-distant but semantically
//     related entity IDs (the "vector jump").
//  5. Unions new IDs into the accumulated seed set.
//
// After all discovery iterations, a final single RetrieveContext call ranks
// the union-of-seeds subgraph (it handles dedup-by-content, ranking, and
// bucket-population uniformly).
//
// opts.MultiHopCount controls iteration count:
//
//	≤0 → default 2 hops. NOTE: this is NEW behaviour. The prior
//	implementation was a strict passthrough regardless of this field's
//	value. Callers that want strict single-hop must set MultiHopCount=1
//	explicitly.
//	=1 → pure passthrough to RetrieveContext; nil vi/embedder allowed
//	≥2 → iterative expansion as described above
//
// Returned *RetrievalResult comes from the FINAL RetrieveContext call, so
// its scoring semantics match a single-hop retrieval exactly. The discovery
// loop only contributes additional seeds.
func MultiHopRetrieveContext(db *sql.DB, vi core.VectorIndex, embedder core.Embedder, seedIDs []string, opts core.RetrieveContextOptions) (*core.RetrievalResult, error) {
	// Empty-seeds short-circuit: matches RetrieveContext's early-return so
	// nil vi/embedder are tolerated when there's nothing to walk.
	if len(seedIDs) == 0 {
		return &core.RetrievalResult{}, nil
	}
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	hops := opts.MultiHopCount
	if hops <= 0 {
		hops = 2
	}

	// Single-hop mode: passthrough. Lets callers use this entrypoint even
	// when vi/embedder are nil (no vector work needed).
	if hops == 1 {
		return RetrieveContext(db, seedIDs, opts)
	}

	if vi == nil || embedder == nil {
		return nil, fmt.Errorf("multi-hop (count=%d) requires non-nil VectorIndex and Embedder", hops)
	} // Tuneables — gofmt-line-up CAPS names so they're greppable from
	// outside the function body. Keep small; multi-hop is a single_bound,
	// not a flood-the-graph operation.
	const (
		MaxTotalMultiHopSeeds = 20 // bounds the SQL IN-clause + final walk size
		TopKPerHop            = 2  // facts selected per hop to drive vector expansion
		VectorTopK            = 3  // neighbours pulled per embedding
		ShallowDepth          = 1  // MaxDepth for hop walks — keeps DB work cheap
	)

	accumulated := make(map[string]bool, len(seedIDs))
	for _, id := range seedIDs {
		accumulated[id] = true
	}
	// currentSeeds holds the NEW seeds discovered in the previous hop —
	// only those are walked at the next hop so walk cost stays bounded.
	currentSeeds := append([]string(nil), seedIDs...)

	hopOpts := opts
	hopOpts.MaxDepth = ShallowDepth

	for h := 1; h < hops; h++ {
		// Cancellation point #1: bail before doing any I/O this hop.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(currentSeeds) == 0 {
			break
		}
		if len(accumulated) > MaxTotalMultiHopSeeds {
			break
		}

		// 1. Shallow walk from current seeds.
		res, err := RetrieveContext(db, currentSeeds, hopOpts)
		if err != nil {
			return nil, fmt.Errorf("multihop hop %d: %w", h, err)
		}

		// 2. Top-K facts across all buckets + seed contents.
		topFacts := topKFromResult(res, TopKPerHop, h == 1)
		if len(topFacts) == 0 {
			break
		}

		// 3. Embed each fact's content.
		// Cancellation point #2: bail before the embed round-trip.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		queryVecs, err := hopEmbedFacts(ctx, embedder, topFacts, h)
		if err != nil {
			return nil, err
		}

		// 4. Vector search for topologically-distant neighbours.
		// Cancellation point #3: bail before the index round-trip.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hits, err := hopVectorSearch(ctx, vi, queryVecs, VectorTopK, h)
		if err != nil {
			return nil, err
		}

		// 5. Merge new IDs (dedup against the accumulated set).
		nextSeeds := hopMergeSeeds(hits, accumulated)
		if len(nextSeeds) == 0 {
			break // no expansion possible; remaining hops would repeat
		}
		currentSeeds = nextSeeds
	} // Final stage: single RetrieveContext with the union of all seeds.
	// It owns dedup-by-content, ranking, and bucket-population so the
	// output shape is identical to a one-hop RetrieveContext call.
	finalSeeds := make([]string, 0, len(accumulated))
	for id := range accumulated {
		finalSeeds = append(finalSeeds, id)
	}
	// Sort for deterministic EXPLAIN reproducibility — Go's map iteration
	// order is randomized, and these IDs feed a SQL IN-clause whose
	// parameter order then influences any depth/category tiebreak.
	sort.Strings(finalSeeds)
	return RetrieveContext(db, finalSeeds, opts)
}

// TODO(retrieval/tests): assert the three loop-break conditions
// (nextSeeds empty, accumulated > MaxTotalMultiHopSeeds, currentSeeds
// empty) and the per-hop seed re-embed redundancy (topKFromResult
// re-emits SeedNode contents every hop).

// topKFromResult picks the top-K facts across the four retrieval buckets,
// optionally including seed contents, ordered by RankingScore descending
// then Content ascending for deterministic selection.
//
// Two idempotency guarantees:
//
//  1. Content-level dedup (within a single call). RetrieveContext ranks
//     seeds into BOTH SeedNodes AND one of the buckets, so the same
//     Content string can surface twice in the helper's input. Without
//     dedup, the topK trim could leave us re-embedding the same content
//     twice within one hop. We collapse on Content string before sort.
//
//  2. includeSeedContents gates the SeedNode-contents append (a separate
//     optimisation for across-hops cases). On hop 1 we keep the seeds so
//     embeddings anchor on the user's actual interests. On later hops
//     those seeds are either already-discovered anchors OR the discoveries
//     themselves — re-embedding them just wastes a round-trip.
func topKFromResult(res *core.RetrievalResult, k int, includeSeedContents bool) []core.RetrievedFact {
	if res == nil || k <= 0 {
		return nil
	}
	seen := make(map[string]bool)
	cap := len(res.WorldFacts) + len(res.Opinions) + len(res.Experiences) + len(res.Observations)
	if includeSeedContents {
		cap += len(res.SeedNodes)
	}
	all := make([]core.RetrievedFact, 0, cap)
	add := func(content string, score float32) {
		if content == "" || seen[content] {
			return
		}
		seen[content] = true
		all = append(all, core.RetrievedFact{Content: content, RankingScore: score})
	}
	// Iteration order below is intentional. walk.go dual-buckets depth-0
	// seeds (SeedNodes AND their category bucket), so the dedup's
	// first-write-wins picks whichever entry arrives FIRST. Putting
	// buckets first means the canonical dedup winner is the bucket entry;
	// both bucket and SeedNode entries derive from the same
	// `rn.score` in walk.go, so the choice is correctness-preserving.
	// Do not reorder — a swap would silently change which entry survives
	// the dedup without changing observable ranking.
	for _, f := range res.WorldFacts {
		add(f.Content, f.RankingScore)
	}
	for _, f := range res.Opinions {
		add(f.Content, f.RankingScore)
	}
	for _, f := range res.Experiences {
		add(f.Content, f.RankingScore)
	}
	for _, f := range res.Observations {
		add(f.Content, f.RankingScore)
	}
	if includeSeedContents {
		for _, n := range res.SeedNodes {
			add(n.Entity.Content, n.RankingScore)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].RankingScore != all[j].RankingScore {
			return all[i].RankingScore > all[j].RankingScore
		}
		return all[i].Content < all[j].Content
	})
	if len(all) > k {
		all = all[:k]
	}
	return all
}

// hopEmbedFacts embeds each fact's content and returns the resulting vectors.
func hopEmbedFacts(ctx context.Context, embedder core.Embedder, facts []core.RetrievedFact, hop int) ([][]float32, error) {
	vecs := make([][]float32, 0, len(facts))
	for _, f := range facts {
		emb, err := embedder.Embed(ctx, f.Content)
		if err != nil {
			return nil, fmt.Errorf("multihop embed hop=%d content=%q: %w", hop, f.Content, err)
		}
		vecs = append(vecs, emb)
	}
	return vecs, nil
}

// hopVectorSearch queries the vector index for neighbours of the given query vectors.
func hopVectorSearch(ctx context.Context, vi core.VectorIndex, queryVecs [][]float32, topK, hop int) ([][]string, error) {
	hits, err := vi.SearchBatch(ctx, queryVecs, topK)
	if err != nil {
		return nil, fmt.Errorf("multihop vector search hop=%d: %w", hop, err)
	}
	return hits, nil
}

// hopMergeSeeds merges newly discovered IDs into the accumulated set and
// returns the next round of seeds (only the truly new ones).
func hopMergeSeeds(hits [][]string, accumulated map[string]bool) []string {
	total := 0
	for _, ids := range hits {
		total += len(ids)
	}
	nextSeeds := make([]string, 0, total)
	for _, ids := range hits {
		for _, id := range ids {
			if !accumulated[id] {
				accumulated[id] = true
				nextSeeds = append(nextSeeds, id)
			}
		}
	}
	return nextSeeds
}
