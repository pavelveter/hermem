package memory

import (
	"context"
	"database/sql"

	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// SearchResult is the fused output of the hybrid (lexical + vector) search.
// Mirrors vector.SearchResult for package-local convenience.
type SearchResult struct {
	ID       string  `json:"id"`
	Category string  `json:"category"`
	Content  string  `json:"content"`
	Score    float64 `json:"score"`
}

// HybridSearch delegates to vector.HybridSearch, re-typing results into the
// memory package's SearchResult alias-equivalent struct.
func HybridSearch(ctx context.Context, db *sql.DB, vi spi.VectorStore, query string, queryEmbedding []float32, limit int) ([]SearchResult, error) {
	res, err := vector.HybridSearch(ctx, db, vi, query, queryEmbedding, limit)
	if err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(res))
	for _, r := range res {
		out = append(out, SearchResult{ID: r.ID, Category: r.Category, Content: r.Content, Score: r.Score})
	}
	return out, nil
}