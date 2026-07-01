package community

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

// makeGraph constructs an in-memory Graph from adjacency data for testing.
func makeGraph(ids []string, edges [][3]interface{}) *Graph {
	n := len(ids)
	idx := make(map[string]int, n)
	for i, id := range ids {
		idx[id] = i
	}
	adj := make([]map[string]float64, n)
	for i := range adj {
		adj[i] = make(map[string]float64)
	}
	nodeWeight := make([]float64, n)
	totalWeight := 0.0
	for _, e := range edges {
		src, dst := e[0].(string), e[1].(string)
		w := e[2].(float64)
		si, oks := idx[src]
		di, okd := idx[dst]
		if oks && okd {
			adj[si][dst] += w
			adj[di][src] += w
			totalWeight += w
		}
	}
	for i := range nodeWeight {
		for _, w := range adj[i] {
			nodeWeight[i] += w
		}
	}
	return &Graph{IDs: ids, NodeIndex: idx, Adj: adj, TotalWeight: totalWeight, NodeWeight: nodeWeight}
}

func TestProperty_DetectCommunities_PartitionCoverage(t *testing.T) {
	g := makeGraph(
		[]string{"a", "b", "c", "d", "e"},
		[][3]interface{}{
			{"a", "b", 1.0}, {"b", "c", 1.0}, {"c", "a", 1.0},
			{"d", "e", 1.0},
		},
	)
	communities, _ := DetectCommunities(g, 10)

	// Property: every node appears in exactly one community.
	seen := make(map[string]bool)
	for _, c := range communities {
		for _, m := range c.Members {
			if seen[m] {
				t.Fatalf("node %s appears in multiple communities", m)
			}
			seen[m] = true
		}
	}
	for _, id := range g.IDs {
		if !seen[id] {
			t.Fatalf("node %s missing from all communities", id)
		}
	}
}

func TestProperty_DetectCommunities_ModularityBounds(t *testing.T) {
	g := makeGraph(
		[]string{"a", "b", "c", "d"},
		[][3]interface{}{
			{"a", "b", 1.0}, {"c", "d", 1.0},
		},
	)
	_, modularity := DetectCommunities(g, 10)

	if modularity < -0.5 || modularity > 1.0 {
		t.Fatalf("modularity %f out of [-0.5, 1.0] bounds", modularity)
	}
}

func TestProperty_DetectCommunities_EmptyGraph(t *testing.T) {
	g := makeGraph(nil, nil)
	communities, modularity := DetectCommunities(g, 10)
	if len(communities) != 0 {
		t.Fatalf("expected empty communities for empty graph, got %d", len(communities))
	}
	if modularity != 0 {
		t.Fatalf("expected modularity 0 for empty graph, got %f", modularity)
	}
}

func TestProperty_DetectCommunities_SingleNode(t *testing.T) {
	g := makeGraph(
		[]string{"a"},
		nil,
	)
	communities, _ := DetectCommunities(g, 10)
	if len(communities) != 1 {
		t.Fatalf("expected 1 community for single node, got %d", len(communities))
	}
	if len(communities[0].Members) != 1 || communities[0].Members[0] != "a" {
		t.Fatalf("expected single member 'a', got %v", communities[0].Members)
	}
}

func TestProperty_DetectCommunities_SortedBySize(t *testing.T) {
	g := makeGraph(
		[]string{"a", "b", "c", "d", "e", "f"},
		[][3]interface{}{
			{"a", "b", 1.0}, {"b", "c", 1.0}, {"c", "a", 1.0},
			{"d", "e", 1.0}, {"e", "f", 1.0}, {"f", "d", 1.0},
		},
	)
	communities, _ := DetectCommunities(g, 10)

	for i := 1; i < len(communities); i++ {
		if communities[i].Size > communities[i-1].Size {
			t.Fatalf("communities not sorted by size: [%d].Size=%d > [%d].Size=%d",
				i, communities[i].Size, i-1, communities[i-1].Size)
		}
	}
}

func TestProperty_DetectCommunities_Deterministic(t *testing.T) {
	g := makeGraph(
		[]string{"a", "b", "c", "d"},
		[][3]interface{}{
			{"a", "b", 1.0}, {"b", "c", 1.0}, {"c", "d", 1.0},
		},
	)
	c1, q1 := DetectCommunities(g, 10)
	c2, q2 := DetectCommunities(g, 10)

	if len(c1) != len(c2) {
		t.Fatalf("non-deterministic: run1=%d communities, run2=%d", len(c1), len(c2))
	}
	for i := range c1 {
		if c1[i].Size != c2[i].Size {
			t.Fatalf("non-deterministic: community %d size %d vs %d", i, c1[i].Size, c2[i].Size)
		}
	}
	if q1 != q2 {
		t.Fatalf("non-deterministic: modularity %f vs %f", q1, q2)
	}
}

func TestProperty_DetectCommunities_communityIDString(t *testing.T) {
	ids := []int{0, 1, 42, 999}
	seen := make(map[string]bool)
	for _, id := range ids {
		s := communityIDString(id)
		if seen[s] {
			t.Fatalf("duplicate community ID string: %s", s)
		}
		seen[s] = true
		if len(s) < 6 || s[:5] != "comm-" {
			t.Fatalf("unexpected community ID format: %s", s)
		}
	}
}

// Verify that core.Community is the expected output type.
var _ core.Community
