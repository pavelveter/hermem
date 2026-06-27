package compression

import (
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
