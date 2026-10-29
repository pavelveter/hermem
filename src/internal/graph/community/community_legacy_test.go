package community

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/pavelveter/hermem/pkg/domain"
)

// legacyDetect is the pre-optimization algorithm, spelled out here as the
// oracle for TestDetectCommunities_MatchesLegacyPartition. DetectCommunities now
// resolves adjacency to integer indices and drops the per-node sort, both of
// which are behaviour-preserving only if the chosen move is identical — a sort
// ordered candidates by community ID, so a linear scan must break ties the same
// way. Keeping the old loop as a test oracle is the cheapest way to prove that
// after any future tuning of the local-moving stage.
func legacyDetect(g *Graph, maxIterations int) ([]domain.Community, float64) {
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
	touched := make([]int, 0)
	for iter := 0; iter < maxIterations; iter++ {
		moved := false
		for i := 0; i < n; i++ {
			oldComm := community[i]
			bestDeltaQ := 0.0
			bestComm := oldComm
			touched = touched[:0]
			for dstStr, w := range g.Adj[i] {
				if di, ok := g.NodeIndex[dstStr]; ok {
					c := community[di]
					if neighbourComms[c] == 0 {
						touched = append(touched, c)
					}
					neighbourComms[c] += w
				}
			}
			commInternal[oldComm] -= 2 * neighbourComms[oldComm]
			commTotal[oldComm] -= g.NodeWeight[i]
			sortedComms := append([]int(nil), touched...)
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

func TestDetectCommunities_MatchesLegacyPartition(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 400; trial++ {
		n := 2 + rng.Intn(120)
		ids := make([]string, n)
		for i := range ids {
			ids[i] = fmt.Sprintf("n%d", i)
		}
		var edges [][3]interface{}
		for e := 0; e < rng.Intn(3*n)+1; e++ {
			a := rng.Intn(n)
			b := rng.Intn(n)
			if a == b {
				continue
			}
			w := float64(1 + rng.Intn(3))
			edges = append(edges, [3]interface{}{ids[a], ids[b], w})
		}
		g := makeGraph(ids, edges)
		got, gq := DetectCommunities(g, 10)
		want, wq := legacyDetect(g, 10)
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(want)
		// globalQ is summed over Go map iteration order in
		// computeGlobalModularity, so its last float digit is not stable even
		// between two runs of the same code. The partition must match exactly.
		dq := gq - wq
		if dq < 0 {
			dq = -dq
		}
		if string(gj) != string(wj) || dq > 1e-9 {
			t.Fatalf("trial %d diverged\nids=%v edges=%v\nnew q=%v %s\nold q=%v %s",
				trial, ids, edges, gq, gj, wq, wj)
		}
	}
}
