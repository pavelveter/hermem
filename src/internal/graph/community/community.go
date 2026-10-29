// Package community implements Louvain community detection on entity graphs.
package community

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pavelveter/hermem/pkg/domain"
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

	edgeRows, err := db.QueryContext(ctx, `SELECT e.source_id, e.target_id, COALESCE(e.weight, 1.0)
		FROM edges e
		JOIN entities se ON se.id = e.source_id AND se.archived = 0
		JOIN entities te ON te.id = e.target_id AND te.archived = 0`)
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

// weightedNeighbour is one resolved adjacency entry: the integer index of the
// neighbouring node plus the summed edge weight to it.
type weightedNeighbour struct {
	to int
	w  float64
}

// indexNeighbours resolves the string-keyed adjacency into integer indices once
// per DetectCommunities call. Nodes whose adjacency points at an ID missing from
// NodeIndex are skipped, matching the previous per-iteration lookup.
func indexNeighbours(g *Graph) [][]weightedNeighbour {
	nbrs := make([][]weightedNeighbour, len(g.Adj))
	for i, adj := range g.Adj {
		nbrs[i] = make([]weightedNeighbour, 0, len(adj))
		for dstStr, w := range adj {
			if di, ok := g.NodeIndex[dstStr]; ok {
				nbrs[i] = append(nbrs[i], weightedNeighbour{to: di, w: w})
			}
		}
	}
	return nbrs
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
func DetectCommunities(g *Graph, maxIterations int) ([]domain.Community, float64) {
	n := len(g.IDs)
	if n == 0 {
		return nil, 0
	}

	community := make([]int, n)
	for i := range community {
		community[i] = i
	}

	commInternal := make([]float64, n)
	commTotal := make([]float64, n)

	for i := range community {
		comm := community[i]
		commTotal[comm] += g.NodeWeight[i]
		for dstStr, w := range g.Adj[i] {
			if di, ok := g.NodeIndex[dstStr]; ok && community[i] == community[di] {
				commInternal[comm] += w
			}
		}
	}

	neighbourComms := make([]float64, n)
	// Resolve the string adjacency to integer indices once. The local-moving
	// loop visits every edge of every node on every pass, and it previously paid
	// a map iteration plus a map lookup into NodeIndex for each one — measured at
	// 40% of the whole pass. Adj is immutable here, so one resolution is enough.
	nbrs := indexNeighbours(g)
	touched := make([]int, 0)
	for iter := 0; iter < maxIterations; iter++ {
		moved := false
		for i := 0; i < n; i++ {
			oldComm := community[i]
			bestDeltaQ := 0.0
			bestComm := oldComm

			touched = touched[:0]
			for _, nb := range nbrs[i] {
				c := community[nb.to]
				if neighbourComms[c] == 0 {
					touched = append(touched, c)
				}
				neighbourComms[c] += nb.w
			}

			commInternal[oldComm] -= 2 * neighbourComms[oldComm]
			commTotal[oldComm] -= g.NodeWeight[i]

			// Pick the best neighbour community in a single pass. The loop used to
			// sort `touched` first purely so the lowest community ID won a tie; a
			// linear scan that breaks ties by lowest ID gives the same choice
			// without the sort, which was 30% of the local-moving stage.
			// A delta of exactly zero never moved the node (bestDeltaQ starts at
			// zero and the old comparison was strict), so ties only break when the
			// move is a genuine improvement.
			for _, newComm := range touched {
				wToComm := neighbourComms[newComm]
				if newComm == oldComm {
					continue
				}
				deltaQ := wToComm/g.TotalWeight - (g.NodeWeight[i]*commTotal[newComm])/(2*g.TotalWeight*g.TotalWeight)
				if deltaQ > bestDeltaQ || (deltaQ > 0 && deltaQ == bestDeltaQ && newComm < bestComm) {
					bestDeltaQ = deltaQ
					bestComm = newComm
				}
			}

			if bestComm != oldComm {
				community[i] = bestComm
				moved = true
			}
			commTotal[bestComm] += g.NodeWeight[i]

			for _, c := range touched {
				neighbourComms[c] = 0
			}
		}
		if !moved {
			break
		}
	}

	return buildResult(g, community), computeGlobalModularity(g, community)
}

// communityIDString formats a community integer ID as "comm-<n>" with
// reduced allocations compared to fmt.Sprintf.
//
// Hot path: buildResult calls this once per detected community (Louvain
// can surface thousands of communities on a dense graph). fmt.Sprintf
// builds a pp formatter via reflection on every call — measured at
// ~3 short-string allocations per call. Replacing the path with a
// pre-grown strings.Builder + strconv.Itoa brings it to ~2 allocations
// (the builder's buffer + the returned string), avoiding reflect
// entirely.
//
// Capacity math: "comm-" prefix (5 bytes) + max int64 decimal length
// (11 bytes for negative numbers, ≤10 for the positive IDs Louvain
// produces via `i = range community` initialisation) + tiny slop. Grow
// pre-allocates the buffer so WriteString does not reallocate.
func communityIDString(commID int) string {
	var b strings.Builder
	b.Grow(20)
	b.WriteString("comm-")
	b.WriteString(strconv.Itoa(commID))
	return b.String()
}

// buildResult maps community IDs to member lists and returns sorted communities.
func buildResult(g *Graph, community []int) []domain.Community {
	commMembers := make([][]string, len(community))
	for i, comm := range community {
		commMembers[comm] = append(commMembers[comm], g.IDs[i])
	}

	var communities []domain.Community
	for commID, members := range commMembers {
		if len(members) == 0 {
			continue
		}
		sort.Strings(members)
		q := computeCommunityModularity(g, community, members)
		communities = append(communities, domain.Community{
			ID:         communityIDString(commID),
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
