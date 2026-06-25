package retrieval

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// RetrieveContext performs a recursive CTE graph walk from seed IDs and returns re-ranked results.
func RetrieveContext(db *sql.DB, seedIDs []string, opts core.RetrieveContextOptions) (*core.RetrievalResult, error) {
	if len(seedIDs) == 0 {
		return &core.RetrievalResult{}, nil
	}
	effDepth := opts.MaxDepth
	if effDepth <= 0 {
		effDepth = 2
	}
	if opts.DepthCeiling > 0 && effDepth > opts.DepthCeiling {
		effDepth = opts.DepthCeiling
	}

	w := opts.RankingWeight.WithDefaults()
	scorer := opts.CompositeScorer
	if scorer == nil {
		scorer = defaultCompositeScorer(w)
	}

	phs, args := store.InClauseArgs(seedIDs)
	var timeFilter string
	if !opts.TimeFrom.IsZero() {
		timeFilter += " AND e.created_at >= ?"
		args = append(args, opts.TimeFrom)
	}
	if !opts.TimeTo.IsZero() {
		timeFilter += " AND e.created_at <= ?"
		args = append(args, opts.TimeTo)
	}
	args = append(args, effDepth)

	query := fmt.Sprintf(`
		WITH RECURSIVE graph_walk AS (
			SELECT e.id, e.category, e.content, e.updated_at, e.embedding, e.degree, 0 as depth, 0.0 as path_weight, '' as parent_id, '' as relation_type, char(31) || e.id || char(31) as visited
			FROM entities e WHERE e.id IN (%s) AND e.archived = 0`+timeFilter+`
			UNION ALL
			SELECT e.id, e.category, e.content, e.updated_at, e.embedding, e.degree, gw.depth + 1, gw.path_weight + COALESCE(ed.weight, 1.0), gw.id, ed.relation_type, gw.visited || e.id || char(31)
			FROM graph_walk gw JOIN edges ed ON (ed.source_id = gw.id OR ed.target_id = gw.id)
			JOIN entities e ON (CASE WHEN ed.source_id = gw.id THEN ed.target_id = e.id ELSE ed.source_id = e.id END)
			WHERE gw.depth < ? AND instr(gw.visited, char(31) || e.id || char(31)) = 0 AND e.archived = 0`+timeFilter+`
		)
		SELECT DISTINCT id, category, content, updated_at, embedding, degree, depth, path_weight, parent_id, relation_type FROM graph_walk ORDER BY depth ASC, category ASC
	`, phs)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("graph walk: %w", err)
	}
	defer rows.Close()

	queryNorm := vector.VectorNorm(opts.QueryEmbedding)
	result := &core.RetrievalResult{
		SeedNodes:    []core.GraphNode{},
		WorldFacts:   []core.RetrievedFact{},
		Opinions:     []core.RetrievedFact{},
		Experiences:  []core.RetrievedFact{},
		Observations: []core.RetrievedFact{},
	}
	seenIDs := make(map[string]bool)
	seenContents := make(map[string]bool)
	var ranked []rankedNode

	for rows.Next() {
		var node core.GraphNode
		var embBlob []byte
		if err := rows.Scan(&node.Entity.ID, &node.Entity.Category, &node.Entity.Content, &node.Entity.UpdatedAt, &embBlob, &node.Entity.Degree, &node.Depth, &node.PathWeight, &node.ParentID, &node.RelationType); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if seenIDs[node.Entity.ID] {
			continue
		}
		seenIDs[node.Entity.ID] = true
		if opts.MaxRetrievedNodes > 0 && len(seenIDs) > opts.MaxRetrievedNodes {
			break
		}

		nodeVec, _ := store.DecodeVector(embBlob, len(opts.QueryEmbedding))
		score := scorer(node, nodeVec, opts.QueryEmbedding, queryNorm)
		node.RankingScore = score
		rn := rankedNode{node: node, score: score}
		if opts.Explain {
			comps := ComputeScoreComponents(node, nodeVec, opts.QueryEmbedding, queryNorm, w)
			rn.sim = comps.Sim
			rn.recency = comps.Recency
			bd := BuildScoreBreakdown(comps, w)
			rn.node.ScoreBreakdown = bd
		}
		ranked = append(ranked, rn)
		if node.Depth == 0 {
			result.SeedNodes = append(result.SeedNodes, node)
		}
	}

	sortByScoreDesc(ranked)

	for _, rn := range ranked {
		if seenContents[rn.node.Entity.Content] {
			continue
		}
		seenContents[rn.node.Entity.Content] = true
		fact := core.RetrievedFact{
			Content:       rn.node.Entity.Content,
			ParentID:      rn.node.ParentID,
			RelationType:  rn.node.RelationType,
			Depth:         rn.node.Depth,
			ScoreBreakdown: rn.node.ScoreBreakdown,
		}
		if opts.Explain {
			fact.VectorScore = rn.sim
			fact.RecencyScore = rn.recency
			fact.DepthPenalty = w.DepthPenalty * rn.node.PathWeight
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
	if opts.Explain {
		logRetrievalExplanation(result, len(seedIDs), effDepth)
	}
	return result, nil
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

		// 2. Top-K facts across all buckets + seed contents, ordered by
		//    RankingScore descending (Content ascending tiebreak).
		topFacts := topKFromResult(res, TopKPerHop, h == 1)
		if len(topFacts) == 0 {
			break
		}

		// 3. Embed each fact's content.
		// Cancellation point #2: bail before the embed round-trip.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		queryVecs := make([][]float32, 0, len(topFacts))
		for _, f := range topFacts {
			emb, err := embedder.Embed(ctx, f.Content)
			if err != nil {
				return nil, fmt.Errorf("multihop embed hop=%d content=%q: %w", h, f.Content, err)
			}
			queryVecs = append(queryVecs, emb)
		}

		// 4. Vector search for topologically-distant neighbours.
		// Cancellation point #3: bail before the index round-trip.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hits, err := vi.SearchBatch(ctx, queryVecs, VectorTopK)
		if err != nil {
			return nil, fmt.Errorf("multihop vector search hop=%d: %w", h, err)
		}

		// 5. Merge new IDs (dedup against the accumulated set).
		nextSeeds := make([]string, 0)
		for _, ids := range hits {
			for _, id := range ids {
				if !accumulated[id] {
					accumulated[id] = true
					nextSeeds = append(nextSeeds, id)
				}
			}
		}
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
	all := make([]core.RetrievedFact, 0)
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
