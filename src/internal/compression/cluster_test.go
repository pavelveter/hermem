package compression

import (
	"fmt"
	"testing"
)

func TestClusterer_EmptyInput(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	c := NewClusterer(db, DefaultClustererConfig())
	clusters, err := c.Cluster(t.Context(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(clusters) != 0 {
		t.Fatalf("expected no clusters, got %d", len(clusters))
	}
}

func TestClusterer_BelowMinSize(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "e1", "world", "content A")
	seedEntity(t, db, "e2", "observation", "content B")
	c := NewClusterer(db, ClustererConfig{
		SimilarityThreshold: 0.75,
		MinClusterSize:      3,
		MaxClusterSize:      10,
	})
	clusters, err := c.Cluster(t.Context(), []string{"e1", "e2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(clusters) != 0 {
		t.Fatalf("expected no clusters, got %d", len(clusters))
	}
}

func TestClusterer_NoEmbeddings(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntity(t, db, "e1", "world", "no embedding")
	c := NewClusterer(db, DefaultClustererConfig())
	clusters, err := c.Cluster(t.Context(), []string{"e1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(clusters) != 0 {
		t.Fatalf("expected no clusters, got %d", len(clusters))
	}
}

func TestClusterer_FixedEmbeddings(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedEntityFull(t, db, "a", "world", "alpha", "", zeroTime, []float32{1, 0, 0})
	seedEntityFull(t, db, "b", "world", "beta", "", zeroTime, []float32{0.95, 0.05, 0})
	seedEntityFull(t, db, "c", "observation", "gamma", "", zeroTime, []float32{0, 1, 0})

	c := NewClusterer(db, ClustererConfig{
		SimilarityThreshold: 0.70,
		MinClusterSize:      2,
		MaxClusterSize:      10,
	})
	clusters, err := c.Cluster(t.Context(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
	memberSet := make(map[string]bool, len(clusters[0]))
	for _, id := range clusters[0] {
		memberSet[id] = true
	}
	if !memberSet["a"] || !memberSet["b"] {
		t.Fatalf("expected cluster [a b], got %v", clusters[0])
	}
}

// TestGreedyCluster_Property_Invariants verifies cluster invariants:
// 1. No entity appears in more than one cluster.
// 2. Every cluster has size >= MinClusterSize.
// 3. No cluster exceeds MaxClusterSize.
// 4. Total members <= total input entities.
func TestGreedyCluster_Property_Invariants(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		name      string
		n         int
		dim       int
		threshold float64
		minSize   int
		maxSize   int
	}{
		{"empty", 0, 3, 0.75, 2, 10},
		{"below_min", 1, 3, 0.75, 2, 10},
		{"all_similar", 10, 3, 0.0, 2, 10},
		{"all_distant", 10, 3, 0.99, 2, 10},
		{"mixed", 20, 5, 0.7, 2, 5},
		{"large_batch", 100, 8, 0.6, 3, 8},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			ids := make([]string, sc.n)
			vecs := make([][]float64, sc.n)
			for i := range ids {
				ids[i] = fmt.Sprintf("e%d", i)
				vecs[i] = make([]float64, sc.dim)
				for j := range vecs[i] {
					vecs[i][j] = float64(i*sc.dim+j) * 0.01
				}
			}

			cfg := ClustererConfig{
				SimilarityThreshold: sc.threshold,
				MinClusterSize:      sc.minSize,
				MaxClusterSize:      sc.maxSize,
			}
			clusters := greedyCluster(ids, vecs, cfg)

			seen := make(map[string]bool)
			totalMembers := 0
			for _, cl := range clusters {
				if len(cl) < sc.minSize {
					t.Errorf("cluster size %d < MinClusterSize %d", len(cl), sc.minSize)
				}
				if len(cl) > sc.maxSize {
					t.Errorf("cluster size %d > MaxClusterSize %d", len(cl), sc.maxSize)
				}
				for _, id := range cl {
					if seen[id] {
						t.Errorf("entity %q appears in multiple clusters", id)
					}
					seen[id] = true
				}
				totalMembers += len(cl)
			}
			if totalMembers > sc.n {
				t.Errorf("total members %d > input count %d", totalMembers, sc.n)
			}
		})
	}
}
