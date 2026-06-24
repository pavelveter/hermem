package retrieval

import (
	"database/sql"
	"fmt"

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
			rn.sim = vector.CosineSimilarityWithNorm(nodeVec, opts.QueryEmbedding, queryNorm)
			rn.recency = recencyScore(node.Entity.UpdatedAt, w.RecencyHalfLifeHours)
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
			Content:      rn.node.Entity.Content,
			ParentID:     rn.node.ParentID,
			RelationType: rn.node.RelationType,
			Depth:        rn.node.Depth,
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
	return result, nil
}

// MultiHopRetrieveContext is a thin wrapper used by GenerateResponse/server handler.
func MultiHopRetrieveContext(db *sql.DB, vi core.VectorIndex, embedder core.Embedder, seedIDs []string, opts core.RetrieveContextOptions) (*core.RetrievalResult, error) {
	_ = vi
	_ = embedder
	return RetrieveContext(db, seedIDs, opts)
}
