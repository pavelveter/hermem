package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"
)

// rankedNode pairs a graph node with the composite score used for
// ordering the category-bucket output. The score is computed once at
// scan time so the post-scan sort is allocation-free.
type rankedNode struct {
	node  GraphNode
	score float32
	// Sprint 2: score breakdown for explainability
	sim     float32
	recency float32
}

// CompositeScorer combines vector similarity, recency decay, and a
// depth penalty into a single deterministic float32 score used to
// order the post-scan category buckets. nil opts.CompositeScorer
// means "use the config-driven default" — see defaultCompositeScorer.
//
// Parameters (per call, one row at a time):
//   - node: the row's GraphNode as scanned from the CTE. node.Entity
//     includes UpdatedAt (recency input) and Category (informational
//     only — default scorer does not weight by category).
//   - nodeVec: the decoded `[]float32` for the row's embedding,
//     precomputed once in the row-scan loop. nil when the row has
//     no embedding or the decode failed; custom scorers treat nil
//     as "no embedding signal available" (this is the only embedding
//     data path exposed to custom scorers — there is no raw-bytes
//     fallback to re-decode from).
//   - queryEmbedding: the user's original query vector (may be nil
//     or empty when the caller asked for a recency-only ranking).
//   - queryNorm: the precomputed L2 norm of queryEmbedding. 0 when
//     the query is missing/empty. Saves one sqrt per row that
//     CosineSimilarityWithNorm would otherwise repeat; the same
//     value is passed to every row in a single retrieval call.
type CompositeScorer func(node GraphNode, nodeVec []float32, queryEmbedding []float32, queryNorm float32) float32

// defaultCompositeScorer returns a CompositeScorer function that uses
// the supplied ranking weights. The returned closure captures weights
// by value so it remains safe across goroutines after RetrieveContext
// returns.
//
// defaultCompositeScorer returns a CompositeScorer function that uses
// the supplied ranking weights. The returned closure captures weights
// by value so it remains safe across goroutines after RetrieveContext
// returns.
func defaultCompositeScorer(w RankingWeight) CompositeScorer {
	return func(
		node GraphNode,
		nodeVec []float32,
		queryEmbedding []float32,
		queryNorm float32,
	) float32 {
		var sim float32
		if len(queryEmbedding) > 0 && len(nodeVec) > 0 {
			sim = CosineSimilarityWithNorm(nodeVec, queryEmbedding, queryNorm)
		}
		var recency float32 = 1
		if !node.Entity.UpdatedAt.IsZero() {
			hoursOld := float32(time.Since(node.Entity.UpdatedAt).Hours())
			if hoursOld > 0 && w.RecencyHalfLifeHours > 0 {
				recency = float32(math.Exp(-float64(hoursOld) / float64(w.RecencyHalfLifeHours)))
			}
		}
		// Temporal reranking: boost facts created near a reference time.
		// Uses the same exponential decay formula as recency but applied
		// to created_at rather than updated_at.
		var temporalBoost float32 = 0
		if node.Entity.CreatedAt != nil && !node.Entity.CreatedAt.IsZero() && w.TemporalHalfLifeHours > 0 {
			hoursOld := float32(time.Since(*node.Entity.CreatedAt).Hours())
			temporalBoost = float32(math.Exp(-float64(hoursOld) / float64(w.TemporalHalfLifeHours)))
		}
		// Centrality boost: log10(1+degree) normalises degree centrality
		// so highly-connected nodes get a mild ranking bonus.
		var centrality float32 = 0
		if w.CentralityWeight > 0 && node.Entity.Degree > 0 {
			centrality = float32(math.Log10(float64(1 + node.Entity.Degree)))
		}
		return compositeScore(w, sim, recency, temporalBoost, centrality, float32(node.PathWeight))
	}
}

// compositeScore combines vector similarity, recency decay, temporal
// boost, centrality, and a per-depth penalty using the config-driven
// RankingWeight values. Formula: VectorWeight*sim + RecencyWeight*recency +
// TemporalWeight*temporalBoost + CentralityWeight*centrality - DepthPenalty*pathWeight.
func compositeScore(w RankingWeight, sim, recency, temporalBoost, centrality, pathWeight float32) float32 {
	return w.VectorWeight*sim +
		w.RecencyWeight*recency +
		w.TemporalWeight*temporalBoost +
		w.CentralityWeight*centrality -
		w.DepthPenalty*pathWeight
}

type GraphNode struct {
	Entity       Entity  `json:"entity"`
	Relations    []Edge  `json:"relations,omitempty"`
	Depth        int     `json:"depth"`
	PathWeight   float32 `json:"path_weight,omitempty"`
	ParentID     string  `json:"parent_id"`
	RelationType string  `json:"relation_type,omitempty"`
	// RankingScore is the composite score used for ordering category-
	// bucket output. Formula uses config-driven RankingWeight:
	// VectorWeight*sim + RecencyWeight*recency + TemporalWeight*temporalBoost +
	// CentralityWeight*log10(1+degree) - DepthPenalty*PathWeight.
	// Defaults to 0.7/0.3/0.05 when zero-valued. 0.0 when the ranker
	// inputs were unavailable. Callers may inspect or sort by it. A
	// custom CompositeScorer may return any float32; post-scan sort
	// orders descending.
	RankingScore float32 `json:"ranking_score"`
}

type RetrievalResult struct {
	SeedNodes    []GraphNode     `json:"seed_nodes"`
	WorldFacts   []RetrievedFact `json:"world_facts"`
	Opinions     []RetrievedFact `json:"opinions"`
	Experiences  []RetrievedFact `json:"experiences"`
	Observations []RetrievedFact `json:"observations"`
}

// RetrievedFact is one re-ranked item in a category bucket. For nodes
// reached via the graph walk (Depth > 0) ParentID and RelationType are
// populated so downstream consumers (FormatContextMarkdown,
// response-generator prompts) can render why this fact was pulled in,
// not just what it says. For seed nodes (Depth == 0) ParentID and
// RelationType are empty strings.
type RetrievedFact struct {
	Content      string `json:"content"`
	ParentID     string `json:"parent_id,omitempty"`
	RelationType string `json:"relation_type,omitempty"`
	Depth        int    `json:"depth"`
	// Sprint 2: retrieval explainability — score breakdown
	VectorScore  float32 `json:"vector_score,omitempty"`
	RecencyScore float32 `json:"recency_score,omitempty"`
	DepthPenalty float32 `json:"depth_penalty,omitempty"`
	RankingScore float32 `json:"ranking_score,omitempty"`
}

// RetrieveContextOptions controls graph-walk bounds for a single
// RetrieveContext call. All fields are optional (zero values are safe
// and mean "use the library defaults / no cap").
type RetrieveContextOptions struct {
	// MaxDepth is the caller's requested depth (<=0 defaults to 2).
	// The actual walk uses min(MaxDepth, DepthCeiling).
	MaxDepth int
	// DepthCeiling clamps MaxDepth; <=0 disables the clamp.
	DepthCeiling int
	// MaxRetrievedNodes soft-caps the total unique entities returned;
	// <=0 disables. May be exceeded by at most one row because the cap
	// is checked after seenIDs updates the running count.
	MaxRetrievedNodes int
	// QueryEmbedding is the user's query vector; used to compute the
	// vector similarity component of the re-rank score. Nil/empty
	// disables vector scoring and falls back to recency-only ranking.
	QueryEmbedding []float32
	// CompositeScorer overrides the default scoring formula.
	// nil → uses defaultCompositeScorer with config-driven
	// RankingWeight. Custom scorers receive the row's GraphNode,
	// decoded nodeVec, query embedding, and precomputed query norm.
	// Signature:
	//
	//	func(node GraphNode,
	//	     nodeVec []float32,
	//	     queryEmbedding []float32,
	//	     queryNorm float32) float32
	CompositeScorer CompositeScorer
	// Ctx carries request-scoped values (request_id) through the
	// retrieval pipeline for structured logging.
	Ctx context.Context
	// Sprint 2: when true, populate vector/recency/depth breakdown
	// on every RetrievedFact. Slight CPU overhead (extra float32
	// assignments) but zero allocations.
	Explain bool
	// Sprint 5: config-driven ranking weights and optional reranker.
	// Zero values are substituted with defaults (0.7 / 0.3 / 0.05 / 720h)
	// at point-of-use for backward compatibility with callers that
	// don't set ranking weights.
	RankingWeight RankingWeight
	Reranker      Reranker
	// QueryText is the original user query string, passed to the
	// reranker for relevance comparison. Optional — reranker skips
	// if empty (e.g. /retrieve endpoint with explicit seed IDs).
	QueryText string
	// Phase 10: multi-hop retrieval — number of search→expand→repeat cycles.
	// 0 or 1 = single hop (default). 2+ = expand from top-scored discovered
	// facts in each hop, embedding their content as new queries.
	MultiHopCount int

	// Phase 10: temporal retrieval — optional time range filter.
	// When non-zero, only entities with created_at in [TimeFrom, TimeTo]
	// are returned. Zero values mean "no bound" (open range).
	TimeFrom time.Time
	TimeTo   time.Time
}

// resolvedRankingWeight returns the effective ranking weights,
// substituting defaults for any zero field.
func resolvedRankingWeight(w RankingWeight) RankingWeight {
	if w.VectorWeight == 0 {
		w.VectorWeight = 0.7
	}
	if w.RecencyWeight == 0 {
		w.RecencyWeight = 0.3
	}
	if w.DepthPenalty == 0 {
		w.DepthPenalty = 0.05
	}
	if w.RecencyHalfLifeHours == 0 {
		w.RecencyHalfLifeHours = 720
	}
	if w.TemporalHalfLifeHours == 0 {
		w.TemporalHalfLifeHours = 720
	}
	if w.CentralityWeight == 0 {
		w.CentralityWeight = 0.05
	}
	return w
}

func RetrieveContext(db *sql.DB, seedIDs []string, opts RetrieveContextOptions) (*RetrievalResult, error) {
	parentCtx := opts.Ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, span := Tracer().Start(parentCtx, "retrieval.graph_walk")
	defer span.End()
	opts.Ctx = ctx

	if len(seedIDs) == 0 {
		return &RetrievalResult{}, nil
	}

	effectiveDepth := opts.MaxDepth
	if effectiveDepth <= 0 {
		effectiveDepth = 2
	}
	if opts.DepthCeiling > 0 && effectiveDepth > opts.DepthCeiling {
		effectiveDepth = opts.DepthCeiling
	}

	// #17: precompute the query embedding's L2 norm once per
	// retrieval call. BatchDotProducts / defaultCompositeScorer
	// need it on every row; caching it here saves one sqrt per row
	// (CosineSimilarity previously recomputed it). 0 when the
	// query embedding is missing/empty, which the default scorer
	// translates into "sim=0" via the normB2/normA guard.
	var queryNorm float32
	if len(opts.QueryEmbedding) > 0 {
		queryNorm = VectorNorm(opts.QueryEmbedding)
	}

	// resolve the scorer once per call: nil opts → default. Assigning
	// a package-level function to a local variable is allocation-free
	// (the function value is statically allocated), so the row loop
	// pays no closure-box allocation cost even when the user supplies
	// their own CompositeScorer.
	w := resolvedRankingWeight(opts.RankingWeight)

	scorer := opts.CompositeScorer
	if scorer == nil {
		scorer = defaultCompositeScorer(w)
	}

	// Seed IDs are inlined into the recursive CTE here rather than
	// routed through execInChunks because the query is a single
	// recursive statement that has to see every seed at once — splitting
	// into separate per-chunk CTEs would walk the graph from each
	// chunk's seeds independently and require a UNION ALL + dedup.
	//
	// Safety bound: SQLite's default SQLITE_MAX_VARIABLE_NUMBER is
	// 999. seedIDs in production paths are bounded by SearchByVector's
	// topK clamp (DefaultSQLBatchSize = 500) plus the soft cap on
	// MaxRetrievedNodes, keeping this IN-clause well under the limit.
	// If a caller passes > 999 seed IDs in the future, this is the
	// place to add a dedup-aware batched variant.
	placeholders := make([]string, len(seedIDs))
	args := make([]interface{}, len(seedIDs))
	for i, id := range seedIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	// Phase 10: temporal retrieval — filter by created_at range.
	// Time args must come BEFORE depth because the CTE anchor arm
	// references them before the recursive arm's depth placeholder.
	var timeFilter string
	if !opts.TimeFrom.IsZero() {
		timeFilter += " AND e.created_at >= ?"
		args = append(args, opts.TimeFrom)
	}
	if !opts.TimeTo.IsZero() {
		timeFilter += " AND e.created_at <= ?"
		args = append(args, opts.TimeTo)
	}

	args = append(args, effectiveDepth)

	// graph_walk recursion terminates on cycles via a `visited`
	// path-column: each row accumulates the IDs of nodes traversed
	// to reach it, and the WHERE clause rejects any expansion
	// whose target appears in the path with
	// `instr(gw.visited, char(31) || e.id || char(31)) = 0`. Without
	// this guard, a 3-cycle A→B→C→A would inflate graph_walk to
	// ~depthCap/3 + 1 rows before the depth cap stops it; SELECT
	// DISTINCT at the end of this query still collapses the
	// user-visible result, so the guard is a
	// correctness/performance improvement at the SQL engine layer
	// (bounded CTE build cost) rather than a user-visible behaviour
	// change. The separate row count check in retrieval_test.go's
	// TestGraphWalk3CycleDirectProbe exercises the SQL guard
	// directly without DISTINCT.
	//
	// Delimiter is `char(31)` (Unit Separator, 0x1F) instead of a
	// printable character: US is structurally absent from any
	// entity id (TSV/IANA-delimiter convention), so the contract is
	// enforced at the SQL semantics layer rather than relying on a
	// "ids never contain ','" social invariant.
	//
	// The 'visited' column is intentionally NOT projected in the
	// final SELECT DISTINCT so retrieval.go's row.Scan signature
	// is unchanged.
	query := fmt.Sprintf(`
		WITH RECURSIVE graph_walk AS (
			SELECT
				e.id, e.category, e.content, e.updated_at, e.embedding,
				e.degree,
				0 as depth,
				0.0 as path_weight,
				'' as parent_id,
				'' as relation_type,
				char(31) || e.id || char(31) as visited
			FROM entities e
			WHERE e.id IN (%[1]s) AND e.archived = 0`+timeFilter+`

		UNION ALL

			SELECT
				e.id, e.category, e.content, e.updated_at, e.embedding,
				e.degree,
				gw.depth + 1,
				gw.path_weight + COALESCE(ed.weight, 1.0),
				gw.id as parent_id,
				ed.relation_type,
				gw.visited || e.id || char(31) as visited
			FROM graph_walk gw
			JOIN edges ed ON (ed.source_id = gw.id OR ed.target_id = gw.id)
			JOIN entities e ON (
				CASE
					WHEN ed.source_id = gw.id THEN ed.target_id = e.id
					ELSE ed.source_id = e.id
				END
			)
			WHERE gw.depth < ?			AND instr(gw.visited, char(31) || e.id || char(31)) = 0
			AND e.archived = 0
			`+timeFilter+`
		)
		SELECT DISTINCT id, category, content, updated_at, embedding, degree, depth, path_weight, parent_id, relation_type
		FROM graph_walk
		ORDER BY depth ASC, category ASC
	`, strings.Join(placeholders, ","))

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute graph retrieval: %w", err)
	}
	defer rows.Close()

	result := &RetrievalResult{
		SeedNodes:    []GraphNode{},
		WorldFacts:   []RetrievedFact{},
		Opinions:     []RetrievedFact{},
		Experiences:  []RetrievedFact{},
		Observations: []RetrievedFact{},
	}

	seenIDs := make(map[string]bool)
	seenContents := make(map[string]bool)
	var ranked []rankedNode

	for rows.Next() {
		var node GraphNode
		var embeddingBlob []byte
		if err := rows.Scan(
			&node.Entity.ID,
			&node.Entity.Category,
			&node.Entity.Content,
			&node.Entity.UpdatedAt,
			&embeddingBlob,
			&node.Entity.Degree,
			&node.Depth,
			&node.PathWeight,
			&node.ParentID,
			&node.RelationType,
		); err != nil {
			return nil, fmt.Errorf("failed to scan graph node: %w", err)
		}

		if !seenIDs[node.Entity.ID] {
			seenIDs[node.Entity.ID] = true
		} else {
			continue
		}

		// Soft cap: stop scanning once we've collected MaxRetrievedNodes
		// unique entities. The check fires after seenIDs updates the
		// running count but BEFORE the row is added to the ranked slice,
		// so the output is bounded at exactly N entities (the trigger row
		// is dropped). The residue seenIDs entry is local to this function
		// and never escapes.
		if opts.MaxRetrievedNodes > 0 && len(seenIDs) > opts.MaxRetrievedNodes {
			break
		}

		// Decode the row's embedding once so the default
		// CompositeScorer doesn't re-decode, and so custom scorers
		// receive a ready-to-use []float32 rather than raw bytes.
		// DecodeVector only succeeds when the query-side length
		// matches the stored dim (an early-write contract); a
		// mismatch becomes nodeVec=nil and the default scorer
		// treats it as "no embedding signal".
		var nodeVec []float32
		if len(opts.QueryEmbedding) > 0 && len(embeddingBlob) > 0 {
			if v, err := DecodeVector(embeddingBlob, len(opts.QueryEmbedding)); err == nil {
				nodeVec = v
			}
		}

		// Compute the re-rank score via the resolved CompositeScorer
		// (custom or default) and stash the node alongside its score
		// so the post-scan sort can produce deterministic category-
		// bucket ordering without a second pass over the result set.
		score := scorer(node, nodeVec, opts.QueryEmbedding, queryNorm)
		node.RankingScore = score
		rn := rankedNode{node: node, score: score}
		// Sprint 2: populate score breakdown when explainability requested
		if opts.Explain {
			if len(opts.QueryEmbedding) > 0 && len(nodeVec) > 0 {
				rn.sim = CosineSimilarityWithNorm(nodeVec, opts.QueryEmbedding, queryNorm)
			}
			if !node.Entity.UpdatedAt.IsZero() {
				hoursOld := float32(time.Since(node.Entity.UpdatedAt).Hours())
				if hoursOld > 0 && w.RecencyHalfLifeHours > 0 {
					rn.recency = float32(math.Exp(-float64(hoursOld) / float64(w.RecencyHalfLifeHours)))
				} else {
					rn.recency = 1.0
				}
			} else {
				rn.recency = 1.0
			}
		}
		ranked = append(ranked, rn)

		// Seed nodes preserve their DB-returned order at depth 0; the
		// score is still attached so downstream consumers can inspect or
		// re-order them.
		if node.Depth == 0 {
			result.SeedNodes = append(result.SeedNodes, node)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating graph rows: %w", err)
	}

	// Single-event summary log emitted at Debug so it stays out of
	// production output by default but is visible on demand. This is
	// the "level-filter as throttle" pattern from TODO §5.3 — one log
	// line per retrieval, not one per row (which would balloon at the
	// configured MaxRetrievedNodes=100 ceiling).
	//
	// Note: we cannot report a "truncated count" here because the
	// soft-cap break fires BEFORE the trigger row is appended to
	// ranked, so len(ranked) never exceeds MaxRetrievedNodes. The
	// `cap_active` flag tells operators the cap was configured and
	// surfaced at least once; the row count itself is the
	// authoritative answer.
	slog.Debug("retrieval complete",
		withReqID(opts.Ctx,
			"event", "retrieval_complete",
			"seed_count", len(seedIDs),
			"total_ranked", len(ranked),
			"effective_depth", effectiveDepth,
			"cap_active", opts.MaxRetrievedNodes > 0,
		)...,
	)

	// Sort once by composite score (descending). SQL return order is
	// the tied-break fallback (deterministic via depth ASC, category ASC
	// in the CTE); SliceStable preserves that fallback for equal scores.
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	// Populate category buckets from the re-ranked slice, still applying
	// seenContents dedup so identical-content entries don't bloat output.
	for _, rn := range ranked {
		if seenContents[rn.node.Entity.Content] {
			continue
		}
		seenContents[rn.node.Entity.Content] = true

		fact := RetrievedFact{
			Content:      rn.node.Entity.Content,
			ParentID:     rn.node.ParentID,
			RelationType: rn.node.RelationType,
			Depth:        rn.node.Depth,
		}
		// Sprint 2: attach score breakdown when explainability is on
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

	// Sprint 5: optional reranker — reorder each category bucket.
	// A nil Reranker (default when no [reranker] config) skips this step.
	if opts.Reranker != nil && opts.QueryText != "" {
		ctx := opts.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		rerankBucket := func(bucket []RetrievedFact) []RetrievedFact {
			if len(bucket) == 0 {
				return bucket
			}
			reranked, err := opts.Reranker.Rerank(ctx, opts.QueryText, bucket)
			if err != nil {
				slog.Warn("reranker failed, keeping original order",
					"event", "reranker_failed",
					"error", err,
					"bucket_len", len(bucket),
				)
				return bucket
			}
			return reranked
		}
		result.WorldFacts = rerankBucket(result.WorldFacts)
		result.Opinions = rerankBucket(result.Opinions)
		result.Experiences = rerankBucket(result.Experiences)
		result.Observations = rerankBucket(result.Observations)
	}

	return result, nil
}

func FormatContextMarkdown(result *RetrievalResult) string {
	if result == nil || len(result.SeedNodes) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Memory Context\n\n")

	writeBucket(&sb, "WORLD", result.WorldFacts)
	writeBucket(&sb, "OPINION", result.Opinions)
	writeBucket(&sb, "EXPERIENCE", result.Experiences)
	writeBucket(&sb, "OBSERVATION", result.Observations)

	return sb.String()
}

// writeBucket renders one re-ranked category slice. For facts reached
// through the graph (Depth > 0) the line includes the relation type
// and parent ID, so the prompt recipient can trace why the fact was
// pulled in. Seed facts (Depth == 0) render as plain content.
func writeBucket(sb *strings.Builder, heading string, facts []RetrievedFact) {
	if len(facts) == 0 {
		return
	}
	sb.WriteString("## " + heading + "\n")
	for _, f := range facts {
		if f.Depth > 0 && f.ParentID != "" {
			sb.WriteString(fmt.Sprintf("- %s (via '%s' from %s)\n", f.Content, f.RelationType, f.ParentID))
		} else {
			sb.WriteString(fmt.Sprintf("- %s\n", f.Content))
		}
	}
	sb.WriteString("\n")
}

// GetExecutableNodes returns pending task nodes whose blocked_by
// dependencies are all completed and whose own status is 'pending'.
// When goalID is non-empty, the result is restricted to tasks
// reachable from that goal via blocked_by edges (the goal's
// transitive dependency tree). When goalID is empty, all leaf-ready
// tasks across the entire graph are returned.
//
// The recursive CTE walks blocked_by edges starting from the goal
// (or all pending tasks when goalID is empty), collecting every
// task in the dependency tree. The outer filter then keeps only
// pending tasks that have zero remaining blockers — tasks whose
// every blocked_by target has status = 'completed'.
func GetExecutableNodes(db *sql.DB, schema SchemaConfig, goalID string) ([]Entity, error) {
	if !schema.StatefulEnabled || len(schema.StatefulCategories) == 0 || len(schema.ValidStateOrder) == 0 {
		return []Entity{}, nil
	}
	if goalID != "" {
		return getExecutableTasksForGoal(db, schema, goalID)
	}
	return getExecutableTasksGlobal(db, schema)
}

func GetExecutableTasks(db *sql.DB, schema SchemaConfig, goalID string) ([]Entity, error) {
	return GetExecutableNodes(db, schema, goalID)
}

func getExecutableTasksForGoal(db *sql.DB, schema SchemaConfig, goalID string) ([]Entity, error) {
	catPH, catArgs := boolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{goalID}, catArgs...)
	args = append(args, schema.RelationBlocking)
	args = append(args, catArgs...)
	args = append(args, schema.ValidStateOrder[0], schema.RelationBlocking, schema.StateUnblocking)
	query := fmt.Sprintf(`
		WITH RECURSIVE dep_tree AS (
			SELECT e.id, e.category, e.content, e.status, e.updated_at
			FROM entities e
			WHERE e.id = ? AND e.category IN (%s) AND e.archived = 0

			UNION ALL

			SELECT e.id, e.category, e.content, e.status, e.updated_at
			FROM dep_tree dt
			JOIN edges ed ON ed.source_id = dt.id AND ed.relation_type = ?
			JOIN entities e ON e.id = ed.target_id AND e.category IN (%s) AND e.archived = 0
		)
		SELECT dt.id, dt.category, dt.content, dt.status, dt.updated_at, COALESCE(e.priority, 0)
		FROM dep_tree dt
		JOIN entities e ON e.id = dt.id
		WHERE dt.status = ?
		AND NOT EXISTS (
			SELECT 1 FROM edges ed2
			WHERE ed2.source_id = dt.id
			AND ed2.relation_type = ?
			AND EXISTS (
				SELECT 1 FROM entities e3
				WHERE e3.id = ed2.target_id
				AND e3.status != ?
			)
		)
		ORDER BY COALESCE(e.priority, 0) DESC, dt.id
	`, catPH, catPH)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get executable tasks for goal %s: %w", goalID, err)
	}
	defer rows.Close()

	return scanTaskEntities(rows)
}

func getExecutableTasksGlobal(db *sql.DB, schema SchemaConfig) ([]Entity, error) {
	catPH, catArgs := boolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{}, catArgs...)
	args = append(args, schema.ValidStateOrder[0], schema.RelationBlocking, schema.StateUnblocking)
	query := fmt.Sprintf(`
		SELECT e.id, e.category, e.content, e.status, e.updated_at, COALESCE(e.priority, 0)
		FROM entities e
		WHERE e.category IN (%s) AND e.status = ? AND e.archived = 0
		AND NOT EXISTS (
			SELECT 1 FROM edges ed
			WHERE ed.source_id = e.id
			AND ed.relation_type = ?
			AND EXISTS (
				SELECT 1 FROM entities e2
				WHERE e2.id = ed.target_id
				AND e2.status != ?
			)
		)
		ORDER BY COALESCE(e.priority, 0) DESC, e.id
	`, catPH)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get executable tasks: %w", err)
	}
	defer rows.Close()

	return scanTaskEntities(rows)
}

func boolMapInClause(values map[string]bool) (string, []interface{}) {
	keys := sortedKeys(values)
	ph := make([]string, len(keys))
	args := make([]interface{}, len(keys))
	for i, key := range keys {
		ph[i] = "?"
		args[i] = key
	}
	return strings.Join(ph, ","), args
}

func scanTaskEntities(rows *sql.Rows) ([]Entity, error) {
	var tasks []Entity
	for rows.Next() {
		var e Entity
		var priority sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &e.Status, &e.UpdatedAt, &priority); err != nil {
			return nil, fmt.Errorf("scan executable task: %w", err)
		}
		if priority.Valid {
			e.Priority = int(priority.Int64)
		}
		tasks = append(tasks, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate executable tasks: %w", err)
	}
	return tasks, nil
}

// FindRollbackTask looks up the recovers_via edge from failedTaskID
// and returns the target entity ID. Returns empty string and nil error
// if no recovery arc is wired.
func FindRollbackNode(db *sql.DB, schema SchemaConfig, failedTaskID string) (string, error) {
	var targetID string
	err := db.QueryRow(
		`SELECT ed.target_id FROM edges ed
		WHERE ed.source_id = ? AND ed.relation_type = ?
		LIMIT 1`,
		failedTaskID, schema.RelationRecovery,
	).Scan(&targetID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find rollback task: %w", err)
	}
	return targetID, nil
}

func FindRollbackTask(db *sql.DB, schema SchemaConfig, failedTaskID string) (string, error) {
	return FindRollbackNode(db, schema, failedTaskID)
}
