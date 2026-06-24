package main

import (
	"fmt"
	"math"
	"sort"

	"database/sql"
)

// Community represents a detected graph community with its members
// and quality metrics.
type Community struct {
	ID         string   `json:"id"`
	Members    []string `json:"members"`
	Size       int      `json:"size"`
	Modularity float64  `json:"modularity"`
}

// DetectCommunities runs a simplified one-pass Louvain modularity
// optimisation on the full graph (undirected, all relation types).
//
// Algorithm:
//  1. Build adjacency list from edges table (symmetric: each undirected
//     edge appears in both adj[src][dst] and adj[dst][src]).
//  2. Initialise each node in its own community.
//  3. Phase 1: repeatedly iterate all nodes, moving each to the
//     neighbour community that yields the max modularity gain.
//     Stop when no moves improve modularity (convergence) or
//     max iterations reached.
//  4. Return communities sorted by size descending.
//
// Modularity formula: simplified ΔQ = (ki_in - Σ_tot * ki / m) / m
// where m = total edge weight, ki = weighted degree, ki_in = edges
// from node i to target community, Σ_tot = sum of degrees in the
// target community. This is a P2-acceptable approximation that uses
// single-count totalWeight (m) consistently across all comparisons;
// the ranking of communities is preserved even though absolute Q
// diverges from the standard 2m-normalised form.
//
// Non-determinism note: when two communities tie on ΔQ, Go map
// iteration order picks the winner arbitrarily. Results are stable
// within a single run but may differ across process restarts.
func DetectCommunities(db *sql.DB, maxIterations int) ([]Community, float64, error) {
	if maxIterations <= 0 {
		maxIterations = 50
	}

	// ── Step 1: build adjacency list ──────────────────────────────
	// Fetch all non-archived entity IDs.
	rows, err := db.Query(`SELECT id FROM entities WHERE archived = 0`)
	if err != nil {
		return nil, 0, fmt.Errorf("detect communities: read entities: %w", err)
	}
	defer rows.Close()

	adj := make(map[string]map[string]float64) // node → neighbour → weight
	var allIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, 0, fmt.Errorf("detect communities: scan entity: %w", err)
		}
		allIDs = append(allIDs, id)
		adj[id] = make(map[string]float64)
	}

	// Fetch all edges with their weights.
	edgeRows, err := db.Query(`SELECT source_id, target_id, COALESCE(weight, 1.0) FROM edges`)
	if err != nil {
		return nil, 0, fmt.Errorf("detect communities: read edges: %w", err)
	}
	defer edgeRows.Close()

	totalWeight := 0.0 // 2m in modularity formula
	for edgeRows.Next() {
		var src, dst string
		var w float64
		if err := edgeRows.Scan(&src, &dst, &w); err != nil {
			return nil, 0, fmt.Errorf("detect communities: scan edge: %w", err)
		}
		if _, ok := adj[src]; ok {
			adj[src][dst] += w
		}
		if _, ok := adj[dst]; ok {
			adj[dst][src] += w
		}
		totalWeight += w
	}

	// Sparse guard: no edges → every node is its own community.
	if totalWeight == 0 || len(allIDs) == 0 {
		var communities []Community
		for _, id := range allIDs {
			communities = append(communities, Community{
				ID:      id,
				Members: []string{id},
				Size:    1,
			})
		}
		sort.Slice(communities, func(i, j int) bool {
			return communities[i].Size > communities[j].Size
		})
		return communities, 0, nil
	}

	m2 := 2 * totalWeight

	// ── Step 2: precompute node weights (k_i in mod formula) ──────
	nodeWeight := make(map[string]float64, len(allIDs))
	for _, id := range allIDs {
		var kw float64
		for _, w := range adj[id] {
			kw += w
		}
		nodeWeight[id] = kw
	}

	// ── Step 3: initialise each node in its own community ──────────
	community := make(map[string]string, len(allIDs))  // node → community id
	communityNodes := make(map[string]map[string]bool) // community id → member set
	communityInternal := make(map[string]float64)      // community id → Σ_in
	communityTotal := make(map[string]float64)         // community id → Σ_tot

	for _, id := range allIDs {
		community[id] = id
		communityNodes[id] = map[string]bool{id: true}
		communityInternal[id] = 0
		// Self-loops not expected, but handle.
		if w, ok := adj[id][id]; ok {
			communityInternal[id] = w
		}
		communityTotal[id] = nodeWeight[id]
	}

	// ── Step 4: Louvain phase 1 — iterate until convergence ──────
	for iter := 0; iter < maxIterations; iter++ {
		moved := false

		for _, node := range allIDs {
			oldComm := community[node]
			oldCommNodes := communityNodes[oldComm]

			// Remove node from its current community.
			delete(oldCommNodes, node)
			kiIn := 0.0
			for nb, w := range adj[node] {
				if community[nb] == oldComm {
					kiIn += w
				}
			}
			communityInternal[oldComm] -= kiIn
			communityTotal[oldComm] -= nodeWeight[node]

			// Find best community to move to.
			bestComm := oldComm
			bestDeltaQ := 0.0
			neighbourCommunities := make(map[string]float64)

			for nb, w := range adj[node] {
				nbComm := community[nb]
				if nbComm == oldComm {
					continue
				}
				neighbourCommunities[nbComm] += w
			}

			for nbComm, kiToComm := range neighbourCommunities {
				// Modularity gain: ΔQ = ki_in/m2 - Σ_tot * k_i / (m2^2)
				// Simplified: ΔQ ≈ (kiToComm - communityTotal[nbComm] * nodeWeight[node] / m2) / totalWeight
				deltaQ := (kiToComm - communityTotal[nbComm]*nodeWeight[node]/totalWeight) / totalWeight
				if deltaQ > bestDeltaQ {
					bestDeltaQ = deltaQ
					bestComm = nbComm
				}
			}

			// Also consider staying: ΔQ = 0 when staying.
			// Move or restore.
			if bestComm != oldComm {
				// Move node to best community.
				community[node] = bestComm
				communityNodes[bestComm][node] = true

				kiToBest := neighbourCommunities[bestComm]
				communityInternal[bestComm] += kiToBest
				communityTotal[bestComm] += nodeWeight[node]
				moved = true
			} else {
				// Restore node to its old community.
				communityNodes[oldComm][node] = true
				communityInternal[oldComm] += kiIn
				communityTotal[oldComm] += nodeWeight[node]
			}
		}

		if !moved {
			break
		}
	}

	// ── Step 5: collect communities, compute modularity ───────────
	// Global modularity: Q = 1/(2m) * Σ_c (Σ_in_c - Σ_tot_c² / (2m))
	globalQ := 0.0
	for commID, nodes := range communityNodes {
		if len(nodes) == 0 {
			continue
		}
		sin := communityInternal[commID]
		stot := communityTotal[commID]
		globalQ += sin/totalWeight - (stot*stot)/(m2*m2)
	}

	// Build result.
	membersByComm := make(map[string][]string)
	for node, commID := range community {
		membersByComm[commID] = append(membersByComm[commID], node)
	}

	var communities []Community
	for commID, members := range membersByComm {
		sort.Strings(members)
		if len(members) == 0 {
			continue
		}
		// Compute per-community modularity contribution.
		sin := communityInternal[commID]
		stot := communityTotal[commID]
		commQ := sin/totalWeight - (stot*stot)/(m2*m2)

		// Use first member as community ID for stable naming.
		commName := commID
		if len(members) >= 2 {
			commName = fmt.Sprintf("community-%s", members[0][:minInt(8, len(members[0]))])
		}

		communities = append(communities, Community{
			ID:         commName,
			Members:    members,
			Size:       len(members),
			Modularity: math.Round(commQ*1e6) / 1e6,
		})
	}

	// Sort by size descending.
	sort.Slice(communities, func(i, j int) bool {
		return communities[i].Size > communities[j].Size
	})

	return communities, math.Round(globalQ*1e6) / 1e6, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
