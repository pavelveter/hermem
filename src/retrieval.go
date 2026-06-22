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

// ranking weights and recency decay constants are package-level so the
// scorer behaves the same across every call site. See TODO §3.
const (
	rankVectorWeight         = 0.7
	rankRecencyWeight        = 0.3
	rankRecencyHalfLifeHours = 30 * 24 // exp decay half-life (720h = 30d)

	// rankScoreMin is the theoretical lower bound on the composite score
	// when cosine similarity hits its floor of -1; used purely for the
	// doc-comment invariant and any future sanity checks.
	rankScoreMin = -rankVectorWeight
)

// rankedNode pairs a graph node with the composite score used for
// ordering the category-bucket output. The score is computed once at
// scan time so the post-scan sort is allocation-free.
type rankedNode struct {
	node  GraphNode
	score float32
}

// computeRankingScore combines vector similarity (cosine) against the
// query and an exponential recency decay (half-life of 30 days). The
// theoretical score range is [rankScoreMin, 1.0] because cosine lives in
// [-1, 1] and recency in (0, 1]. Missing inputs degrade gracefully
// rather than abort: empty OR absent embedding bytes on either side →
// sim=0; UpdatedAt.IsZero() (defensive for test fixtures) → recency=1
// (treat as fresher than any real timestamp).
func computeRankingScore(queryEmbedding []float32, nodeEmbedding []byte, updatedAt time.Time) float32 {
	var sim float32
	if len(queryEmbedding) > 0 && len(nodeEmbedding) > 0 {
		if emb, err := DecodeVector(nodeEmbedding, len(queryEmbedding)); err == nil {
			sim = CosineSimilarity(queryEmbedding, emb)
		}
	}
	var recency float32 = 1
	if !updatedAt.IsZero() {
		hoursOld := float32(time.Since(updatedAt).Hours())
		if hoursOld > 0 {
			recency = float32(math.Exp(-float64(hoursOld) / float64(rankRecencyHalfLifeHours)))
		}
	}
	return rankVectorWeight*sim + rankRecencyWeight*recency
}

type GraphNode struct {
	Entity       Entity `json:"entity"`
	Relations    []Edge `json:"relations,omitempty"`
	Depth        int    `json:"depth"`
	ParentID     string `json:"parent_id"`
	RelationType string `json:"relation_type,omitempty"`
	// RankingScore is the deterministic composite score used for ordering
	// category-bucket output. It is computed as 0.7*sim + 0.3*recency.
	// 0.0 when the ranker inputs were unavailable (no QueryEmbedding and
	// no UpdatedAt). Callers may inspect or sort by it, but the canonical
	// ordering rule is the internal re-rank after scan.
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
	// Ctx carries request-scoped values (request_id) through the
	// retrieval pipeline for structured logging.
	Ctx context.Context
}

func RetrieveContext(db *sql.DB, seedIDs []string, opts RetrieveContextOptions) (*RetrievalResult, error) {
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
				0 as depth,
				'' as parent_id,
				'' as relation_type,
				char(31) || e.id || char(31) as visited
			FROM entities e
			WHERE e.id IN (%[1]s) AND e.archived = 0

			UNION ALL

			SELECT
				e.id, e.category, e.content, e.updated_at, e.embedding,
				gw.depth + 1,
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
			WHERE gw.depth < ?
				AND instr(gw.visited, char(31) || e.id || char(31)) = 0
				AND e.archived = 0
		)
		SELECT DISTINCT id, category, content, updated_at, embedding, depth, parent_id, relation_type
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
			&node.Depth,
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

		// Compute the re-rank score from query similarity + recency and
		// stash the node alongside its score so the post-scan sort can
		// produce deterministic category-bucket ordering without a second
		// pass over the result set.
		score := computeRankingScore(opts.QueryEmbedding, embeddingBlob, node.Entity.UpdatedAt)
		node.RankingScore = score
		ranked = append(ranked, rankedNode{node: node, score: score})

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
