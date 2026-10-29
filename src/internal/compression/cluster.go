package compression

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	"github.com/pavelveter/hermem/src/internal/store"
)

type ClustererConfig struct {
	SimilarityThreshold float64
	MinClusterSize      int
	MaxClusterSize      int
}

func DefaultClustererConfig() ClustererConfig {
	return ClustererConfig{
		SimilarityThreshold: 0.75,
		MinClusterSize:      2,
		MaxClusterSize:      10,
	}
}

type Clusterer struct {
	db     *sql.DB
	config ClustererConfig
}

func NewClusterer(db *sql.DB, config ClustererConfig) *Clusterer {
	return &Clusterer{db: db, config: config}
}

func (c *Clusterer) Cluster(ctx context.Context, entityIDs []string) ([][]string, error) {
	if len(entityIDs) < c.config.MinClusterSize {
		return nil, nil
	}
	embeddings, err := c.loadEmbeddings(ctx, entityIDs)
	if err != nil {
		return nil, fmt.Errorf("cluster: load embeddings: %w", err)
	}
	if len(embeddings) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(embeddings))
	vecs := make([][]float32, 0, len(embeddings))
	for id, vec := range embeddings {
		ids = append(ids, id)
		vecs = append(vecs, vec)
	}

	return greedyCluster(ids, vecs, c.config), nil
}

// greedyCluster assigns entities to clusters using a greedy single-pass
// algorithm. Each unassigned entity becomes a seed; similar entities
// (above SimilarityThreshold) are absorbed into its cluster.
func greedyCluster(ids []string, vecs [][]float32, cfg ClustererConfig) [][]string {
	pool := make([]int, len(ids))
	for i := range pool {
		pool[i] = i
	}
	clusters := make([][]string, 0)
	for len(pool) > 0 {
		seed := pool[0]
		pool = pool[1:]
		cluster := []int{seed}
		var remaining []int
		for _, idx := range pool {
			sim := cosineSimilarity(vecs[seed], vecs[idx])
			if sim >= cfg.SimilarityThreshold {
				cluster = append(cluster, idx)
			} else {
				remaining = append(remaining, idx)
			}
		}
		pool = remaining
		if len(cluster) >= cfg.MinClusterSize {
			if len(cluster) > cfg.MaxClusterSize {
				cluster = cluster[:cfg.MaxClusterSize]
			}
			members := make([]string, len(cluster))
			for i, idx := range cluster {
				members[i] = ids[idx]
			}
			clusters = append(clusters, members)
		}
	}
	return clusters
}

func (c *Clusterer) loadEmbeddings(ctx context.Context, ids []string) (map[string][]float32, error) {
	phs, args := store.InClauseArgs(ids)
	query := "SELECT id, embedding FROM entities WHERE id IN (" + phs + ") AND embedding IS NOT NULL"
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]float32, len(ids))
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}
		vec := store.BytesToEmbedding(blob)
		if vec == nil {
			continue
		}
		result[id] = vec
	}
	return result, rows.Err()
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		x := float64(a[i])
		y := float64(b[i])
		dot += x * y
		normA += x * x
		normB += y * y
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
