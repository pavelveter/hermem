package store

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/pavelveter/hermem/src/internal/core"
)

// DetectCommunities runs Louvain community detection on the graph.
func DetectCommunities(db *sql.DB, maxIterations int) ([]core.Community, float64, error) {
	// Fetch all non-archived entity IDs.
	rows, err := db.Query(`SELECT id FROM entities WHERE archived = 0`)
	if err != nil {
		return nil, 0, fmt.Errorf("community: entities: %w", err)
	}
	defer rows.Close()
	var ids []string
	nodeIndex := make(map[string]int)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, 0, fmt.Errorf("community: scan: %w", err)
		}
		ids = append(ids, id)
		nodeIndex[id] = len(ids) - 1
	}
	n := len(ids)
	if n == 0 {
		return nil, 0, nil
	}

	// Build adjacency list with weights from edges.
	adj := make([]map[string]float64, n)
	for i := range adj {
		adj[i] = make(map[string]float64)
	}
	edgeRows, err := db.Query(`SELECT source_id, target_id, COALESCE(weight, 1.0) FROM edges`)
	if err != nil {
		return nil, 0, fmt.Errorf("community: edges: %w", err)
	}
	defer edgeRows.Close()
	totalWeight := 0.0
	for edgeRows.Next() {
		var src, dst string
		var w float64
		if err := edgeRows.Scan(&src, &dst, &w); err != nil {
			return nil, 0, fmt.Errorf("community: scan edge: %w", err)
		}
		si, oks := nodeIndex[src]
		di, okd := nodeIndex[dst]
		if oks && okd {
			adj[si][dst] += w
			adj[di][src] += w
			totalWeight += w
		}
	}

	// Louvain phase 1: initialise each node in its own community.
	community := make([]int, n)
	for i := range community {
		community[i] = i
	}

	// Weights within and total weights for each community.
	commInternal := make(map[int]float64)
	commTotal := make(map[int]float64)

	// Node total weights.
	nodeWeight := make([]float64, n)
	for i := range nodeWeight {
		for _, w := range adj[i] {
			nodeWeight[i] += w
		}
	}

	// Initialise community weights.
	for i := range community {
		comm := community[i]
		commTotal[comm] += nodeWeight[i]
		for dstStr, w := range adj[i] {
			if di, ok := nodeIndex[dstStr]; ok && community[i] == community[di] {
				commInternal[comm] += w
			}
		}
	}

	// Run Louvain iterations.
	for iter := 0; iter < maxIterations; iter++ {
		moved := false
		for i := 0; i < n; i++ {
			oldComm := community[i]
			bestDeltaQ := 0.0
			bestComm := oldComm

			// Compute weight from i to each neighbouring community.
			neighbourComms := make(map[int]float64)
			for dstStr, w := range adj[i] {
				if di, ok := nodeIndex[dstStr]; ok {
					neighbourComms[community[di]] += w
				}
			}

			// Remove i from its current community weights.
			commInternal[oldComm] -= 2 * neighbourComms[oldComm]
			commTotal[oldComm] -= nodeWeight[i]

			for newComm, wToComm := range neighbourComms {
				if newComm == oldComm {
					continue
				}
				deltaQ := wToComm/totalWeight - (nodeWeight[i]*commTotal[newComm])/(2*totalWeight*totalWeight)
				if deltaQ > bestDeltaQ {
					bestDeltaQ = deltaQ
					bestComm = newComm
				}
			}

			// Add i back (to old or new community).
			if bestComm != oldComm {
				community[i] = bestComm
				moved = true
			}
			commTotal[bestComm] += nodeWeight[i]
		}
		if !moved {
			break
		}
	}

	// Build result: map community ID to member list.
	commMembers := make(map[int][]string)
	for i, comm := range community {
		commMembers[comm] = append(commMembers[comm], ids[i])
	}

	// Compute modularity.
	globalQ := 0.0
	for i := 0; i < n; i++ {
		for dstStr, w := range adj[i] {
			if di, ok := nodeIndex[dstStr]; ok {
				if community[i] == community[di] {
					globalQ += w - nodeWeight[i]*nodeWeight[di]/(2*totalWeight)
				}
			}
		}
	}
	if totalWeight > 0 {
		globalQ /= (2 * totalWeight)
	}

	// Build sorted result.
	var communities []core.Community
	for commID, members := range commMembers {
		sort.Strings(members)
		// Compute per-community modularity.
		q := 0.0
		for _, mi := range members {
			for _, mj := range members {
				if mi == mj {
					continue
				}
				ii := nodeIndex[mi]
				jj := nodeIndex[mj]
				w := adj[ii][mj]
				q += w - nodeWeight[ii]*nodeWeight[jj]/(2*totalWeight)
			}
		}
		if totalWeight > 0 {
			q /= (2 * totalWeight)
		}
		communities = append(communities, core.Community{
			ID:         fmt.Sprintf("comm-%d", commID),
			Members:    members,
			Size:       len(members),
			Modularity: q,
		})
	}
	sort.Slice(communities, func(i, j int) bool { return communities[i].Size > communities[j].Size })
	return communities, globalQ, nil
}
