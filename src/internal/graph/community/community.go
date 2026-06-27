// Package community implements Louvain community detection on entity graphs.
package community

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/pavelveter/hermem/src/internal/core"
)

// Graph holds the in-memory adjacency representation of the entity graph.
type Graph struct {
	IDs         []string
	NodeIndex   map[string]int
	Adj         []map[string]float64
	TotalWeight float64
	NodeWeight  []float64
}

// LoadGraph fetches all non-archived entities and their weighted edges from
// the database and returns an in-memory Graph ready for community detection.
func LoadGraph(ctx context.Context, db *sql.DB) (*Graph, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM entities WHERE archived = 0`)
	if err != nil {
		return nil, fmt.Errorf("community: entities: %w", err)
	}
	defer rows.Close()

	var ids []string
	nodeIndex := make(map[string]int)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("community: scan: %w", err)
		}
		ids = append(ids, id)
		nodeIndex[id] = len(ids) - 1
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("community: rows: %w", err)
	}
	n := len(ids)
	if n == 0 {
		return nil, nil
	}

	adj := make([]map[string]float64, n)
	for i := range adj {
		adj[i] = make(map[string]float64)
	}

	edgeRows, err := db.QueryContext(ctx, `SELECT source_id, target_id, COALESCE(weight, 1.0) FROM edges`)
	if err != nil {
		return nil, fmt.Errorf("community: edges: %w", err)
	}
	defer edgeRows.Close()

	totalWeight := 0.0
	for edgeRows.Next() {
		var src, dst string
		var w float64
		if err := edgeRows.Scan(&src, &dst, &w); err != nil {
			return nil, fmt.Errorf("community: scan edge: %w", err)
		}
		si, oks := nodeIndex[src]
		di, okd := nodeIndex[dst]
		if oks && okd {
			adj[si][dst] += w
			adj[di][src] += w
			totalWeight += w
		}
	}
	if err := edgeRows.Err(); err != nil {
		return nil, fmt.Errorf("community: edge rows: %w", err)
	}

	nodeWeight := make([]float64, n)
	for i := range nodeWeight {
		for _, w := range adj[i] {
			nodeWeight[i] += w
		}
	}

	return &Graph{
		IDs:         ids,
		NodeIndex:   nodeIndex,
		Adj:         adj,
		TotalWeight: totalWeight,
		NodeWeight:  nodeWeight,
	}, nil
}

// DetectCommunities runs Louvain community detection on an in-memory Graph
// for up to maxIterations passes. Returns the communities sorted by size
// (largest first) and the global modularity score.
//
// Thread-safety: the caller MUST pass a snapshot Graph obtained via
// LoadGraph within a read-locked section. DetectCommunities only reads
// g.IDs, g.Adj, g.NodeIndex, g.TotalWeight, and g.NodeWeight — it
// never mutates the graph. All mutable state (community assignments,
// commInternal, commTotal) lives in stack-local slices/maps.
func DetectCommunities(g *Graph, maxIterations int) ([]core.Community, float64) {
	n := len(g.IDs)
	if n == 0 {
		return nil, 0
	}

	community := make([]int, n)
	for i := range community {
		community[i] = i
	}

	commInternal := make(map[int]float64)
	commTotal := make(map[int]float64)

	for i := range community {
		comm := community[i]
		commTotal[comm] += g.NodeWeight[i]
		for dstStr, w := range g.Adj[i] {
			if di, ok := g.NodeIndex[dstStr]; ok && community[i] == community[di] {
				commInternal[comm] += w
			}
		}
	}

	for iter := 0; iter < maxIterations; iter++ {
		moved := false
		for i := 0; i < n; i++ {
			oldComm := community[i]
			bestDeltaQ := 0.0
			bestComm := oldComm

			neighbourComms := make(map[int]float64)
			for dstStr, w := range g.Adj[i] {
				if di, ok := g.NodeIndex[dstStr]; ok {
					neighbourComms[community[di]] += w
				}
			}

			commInternal[oldComm] -= 2 * neighbourComms[oldComm]
			commTotal[oldComm] -= g.NodeWeight[i]

			sortedComms := make([]int, 0, len(neighbourComms))
			for c := range neighbourComms {
				sortedComms = append(sortedComms, c)
			}
			sort.Ints(sortedComms)
			for _, newComm := range sortedComms {
				wToComm := neighbourComms[newComm]
				if newComm == oldComm {
					continue
				}
				deltaQ := wToComm/g.TotalWeight - (g.NodeWeight[i]*commTotal[newComm])/(2*g.TotalWeight*g.TotalWeight)
				if deltaQ > bestDeltaQ {
					bestDeltaQ = deltaQ
					bestComm = newComm
				}
			}

			if bestComm != oldComm {
				community[i] = bestComm
				moved = true
			}
			commTotal[bestComm] += g.NodeWeight[i]
		}
		if !moved {
			break
		}
	}

	return buildResult(g, community), computeGlobalModularity(g, community)
}

// buildResult maps community IDs to member lists and returns sorted communities.
func buildResult(g *Graph, community []int) []core.Community {
	commMembers := make(map[int][]string)
	for i, comm := range community {
		commMembers[comm] = append(commMembers[comm], g.IDs[i])
	}

	var communities []core.Community
	for commID, members := range commMembers {
		sort.Strings(members)
		q := computeCommunityModularity(g, community, members)
		communities = append(communities, core.Community{
			ID:         fmt.Sprintf("comm-%d", commID),
			Members:    members,
			Size:       len(members),
			Modularity: q,
		})
	}
	sort.Slice(communities, func(i, j int) bool { return communities[i].Size > communities[j].Size })
	return communities
}

// computeGlobalModularity calculates the overall modularity of the partition.
func computeGlobalModularity(g *Graph, community []int) float64 {
	n := len(g.IDs)
	globalQ := 0.0
	for i := 0; i < n; i++ {
		for dstStr, w := range g.Adj[i] {
			if di, ok := g.NodeIndex[dstStr]; ok {
				if community[i] == community[di] {
					globalQ += w - g.NodeWeight[i]*g.NodeWeight[di]/(2*g.TotalWeight)
				}
			}
		}
	}
	if g.TotalWeight > 0 {
		globalQ /= (2 * g.TotalWeight)
	}
	return globalQ
}

// computeCommunityModularity calculates modularity for a single community.
func computeCommunityModularity(g *Graph, community []int, members []string) float64 {
	q := 0.0
	for _, mi := range members {
		for _, mj := range members {
			if mi == mj {
				continue
			}
			ii := g.NodeIndex[mi]
			jj := g.NodeIndex[mj]
			w := g.Adj[ii][mj]
			q += w - g.NodeWeight[ii]*g.NodeWeight[jj]/(2*g.TotalWeight)
		}
	}
	if g.TotalWeight > 0 {
		q /= (2 * g.TotalWeight)
	}
	return q
}
