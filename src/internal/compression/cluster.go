package compression

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
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
	vecs := make([][]float64, 0, len(embeddings))
	for id, vec := range embeddings {
		ids = append(ids, id)
		vecs = append(vecs, vec)
	}

	pool := make([]int, len(ids))
	for i := range pool {
		pool[i] = i
	}
	var clusters [][]string
	for len(pool) > 0 {
		seed := pool[0]
		pool = pool[1:]
		cluster := []int{seed}
		var remaining []int
		for _, idx := range pool {
			sim := cosineSimilarity(vecs[seed], vecs[idx])
			if sim >= c.config.SimilarityThreshold {
				cluster = append(cluster, idx)
			} else {
				remaining = append(remaining, idx)
			}
		}
		pool = remaining
		if len(cluster) >= c.config.MinClusterSize {
			if len(cluster) > c.config.MaxClusterSize {
				cluster = cluster[:c.config.MaxClusterSize]
			}
			members := make([]string, len(cluster))
			for i, idx := range cluster {
				members[i] = ids[idx]
			}
			clusters = append(clusters, members)
		}
	}
	return core.NormalizeSlice(clusters), nil
}

func (c *Clusterer) loadEmbeddings(ctx context.Context, ids []string) (map[string][]float64, error) {
	phs, args := store.InClauseArgs(ids)
	query := fmt.Sprintf("SELECT id, embedding FROM entities WHERE id IN (%s) AND embedding IS NOT NULL", phs)
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]float64, len(ids))
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
		f64 := make([]float64, len(vec))
		for i, v := range vec {
			f64[i] = float64(v)
		}
		result[id] = f64
	}
	return result, rows.Err()
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	f32a := make([]float32, len(a))
	f32b := make([]float32, len(b))
	for i := range a {
		f32a[i] = float32(a[i])
		f32b[i] = float32(b[i])
	}
	return float64(vector.CosineSimilarity(f32a, f32b))
}
