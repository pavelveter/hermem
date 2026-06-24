// Package retrieval provides graph-walk context retrieval with composite scoring.
package retrieval

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// rankedNode pairs a graph node with its composite score.
type rankedNode struct {
	node    core.GraphNode
	score   float32
	sim     float32
	recency float32
}

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

	var queryNorm float32
	if len(opts.QueryEmbedding) > 0 {
		queryNorm = vector.VectorNorm(opts.QueryEmbedding)
	}

	w := resolvedRankingWeight(opts.RankingWeight)
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

	result := &core.RetrievalResult{
		SeedNodes: []core.GraphNode{}, WorldFacts: []core.RetrievedFact{},
		Opinions: []core.RetrievedFact{}, Experiences: []core.RetrievedFact{}, Observations: []core.RetrievedFact{},
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

		var nodeVec []float32
		if len(opts.QueryEmbedding) > 0 && len(embBlob) > 0 {
			if v, err := store.DecodeVector(embBlob, len(opts.QueryEmbedding)); err == nil {
				nodeVec = v
			}
		}
		score := scorer(node, nodeVec, opts.QueryEmbedding, queryNorm)
		node.RankingScore = score
		rn := rankedNode{node: node, score: score}
		if opts.Explain {
			if len(opts.QueryEmbedding) > 0 && len(nodeVec) > 0 {
				rn.sim = vector.CosineSimilarityWithNorm(nodeVec, opts.QueryEmbedding, queryNorm)
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
		if node.Depth == 0 {
			result.SeedNodes = append(result.SeedNodes, node)
		}
	}

	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	for _, rn := range ranked {
		if seenContents[rn.node.Entity.Content] {
			continue
		}
		seenContents[rn.node.Entity.Content] = true
		fact := core.RetrievedFact{Content: rn.node.Entity.Content, ParentID: rn.node.ParentID, RelationType: rn.node.RelationType, Depth: rn.node.Depth}
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

// MultiHopRetrieveContext performs multi-hop retrieval with re-embedding.
func MultiHopRetrieveContext(db *sql.DB, vi core.VectorIndex, embedder core.Embedder, seedIDs []string, opts core.RetrieveContextOptions) (*core.RetrievalResult, error) {
	return RetrieveContext(db, seedIDs, opts)
}

func resolvedRankingWeight(w core.RankingWeight) core.RankingWeight {
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

func defaultCompositeScorer(w core.RankingWeight) core.CompositeScorer {
	return func(node core.GraphNode, nodeVec []float32, queryEmbedding []float32, queryNorm float32) float32 {
		var sim float32
		if len(queryEmbedding) > 0 && len(nodeVec) > 0 {
			sim = vector.CosineSimilarityWithNorm(nodeVec, queryEmbedding, queryNorm)
		}
		var recency float32 = 1
		if !node.Entity.UpdatedAt.IsZero() {
			hoursOld := float32(time.Since(node.Entity.UpdatedAt).Hours())
			if hoursOld > 0 && w.RecencyHalfLifeHours > 0 {
				recency = float32(math.Exp(-float64(hoursOld) / float64(w.RecencyHalfLifeHours)))
			}
		}
		var temporalBoost float32 = 0
		if node.Entity.CreatedAt != nil && !node.Entity.CreatedAt.IsZero() && w.TemporalHalfLifeHours > 0 {
			temporalBoost = float32(math.Exp(-float64(time.Since(*node.Entity.CreatedAt).Hours()) / float64(w.TemporalHalfLifeHours)))
		}
		var centrality float32 = 0
		if w.CentralityWeight > 0 && node.Entity.Degree > 0 {
			centrality = float32(math.Log10(float64(1 + node.Entity.Degree)))
		}
		return compositeScore(w, sim, recency, temporalBoost, centrality, node.PathWeight)
	}
}

func compositeScore(w core.RankingWeight, sim, recency, temporalBoost, centrality, pathWeight float32) float32 {
	return w.VectorWeight*sim + w.RecencyWeight*recency + w.TemporalWeight*temporalBoost + w.CentralityWeight*centrality - w.DepthPenalty*pathWeight
}

// FormatContextMarkdown renders a RetrievalResult as markdown.
func FormatContextMarkdown(result *core.RetrievalResult) string {
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

func writeBucket(sb *strings.Builder, heading string, facts []core.RetrievedFact) {
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

// GetExecutableTasks returns pending tasks with all blockers completed.
func GetExecutableTasks(db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Entity, error) {
	if !schema.StatefulEnabled || len(schema.StatefulCategories) == 0 || len(schema.ValidStateOrder) == 0 {
		return []core.Entity{}, nil
	}
	if goalID != "" {
		return getExecutableForGoal(db, schema, goalID)
	}
	return getExecutableGlobal(db, schema)
}

func getExecutableForGoal(db *sql.DB, schema core.SchemaConfig, goalID string) ([]core.Entity, error) {
	catPH, catArgs := store.BoolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{goalID}, catArgs...)
	args = append(args, schema.RelationBlocking)
	args = append(args, catArgs...)
	args = append(args, schema.ValidStateOrder[0], schema.RelationBlocking, schema.StateUnblocking)
	query := fmt.Sprintf(`WITH RECURSIVE dep_tree AS (SELECT e.id, e.category, e.content, e.status, e.updated_at FROM entities e WHERE e.id = ? AND e.category IN (%s) AND e.archived = 0 UNION ALL SELECT e.id, e.category, e.content, e.status, e.updated_at FROM dep_tree dt JOIN edges ed ON ed.source_id = dt.id AND ed.relation_type = ? JOIN entities e ON e.id = ed.target_id AND e.category IN (%s) AND e.archived = 0) SELECT dt.id, dt.category, dt.content, dt.status, dt.updated_at, COALESCE(e.priority, 0) FROM dep_tree dt JOIN entities e ON e.id = dt.id WHERE dt.status = ? AND NOT EXISTS (SELECT 1 FROM edges ed2 WHERE ed2.source_id = dt.id AND ed2.relation_type = ? AND EXISTS (SELECT 1 FROM entities e3 WHERE e3.id = ed2.target_id AND e3.status != ?)) ORDER BY COALESCE(e.priority, 0) DESC, dt.id`, catPH, catPH)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("executable for goal: %w", err)
	}
	defer rows.Close()
	return store.ScanTaskEntities(rows)
}

func getExecutableGlobal(db *sql.DB, schema core.SchemaConfig) ([]core.Entity, error) {
	catPH, catArgs := store.BoolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{}, catArgs...)
	args = append(args, schema.ValidStateOrder[0], schema.RelationBlocking, schema.StateUnblocking)
	query := fmt.Sprintf(`SELECT e.id, e.category, e.content, e.status, e.updated_at, COALESCE(e.priority, 0) FROM entities e WHERE e.category IN (%s) AND e.status = ? AND e.archived = 0 AND NOT EXISTS (SELECT 1 FROM edges ed WHERE ed.source_id = e.id AND ed.relation_type = ? AND EXISTS (SELECT 1 FROM entities e2 WHERE e2.id = ed.target_id AND e2.status != ?)) ORDER BY COALESCE(e.priority, 0) DESC, e.id`, catPH)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("executable: %w", err)
	}
	defer rows.Close()
	return store.ScanTaskEntities(rows)
}
