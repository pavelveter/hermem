package vector

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
)

// Compile-time interface assertion.
var _ core.VectorIndex = (*SQLiteVecIndex)(nil)

// SQLiteVecIndex is an optional VectorIndex implementation backed by
// the sqlite-vec SQLite extension. It uses SQL-native vector similarity
// search instead of the in-memory brute-force approach.
//
// Architecture:
//
//	core.VectorIndex (interface)
//	  ├── InMemoryVectorIndex  (default, brute-force cosine)
//	  └── SQLiteVecIndex       (sqlite-vec extension, ANN search)
//
// Future implementations can follow this same pattern:
//   - HNSWIndex (github.com/hypermodeinc/hnswlib)
//   - QdrantIndex (Qdrant HTTP API)
//   - PGVectorIndex (pgvector SQL extension)
//   - FAISSIndex (Facebook FAISS via CGo)
//
// IMPORTANT: Retrieval logic must NOT be coupled to SQLiteVecIndex.
// All code depends only on core.VectorIndex.
//
// To enable: set vector_backend = "sqlite-vec" in hermem.ini.
// Requires the sqlite-vec SQLite extension to be loaded at runtime.
type SQLiteVecIndex struct {
	db     *sql.DB
	dim    int
	loaded bool
}

// NewSQLiteVecIndex creates a SQLiteVecIndex. The sqlite-vec extension
// must already be loaded into the DB connection pool.
//
// Returns an error if the sqlite-vec module is not available, so
// callers can fall back to InMemoryVectorIndex gracefully.
func NewSQLiteVecIndex(db *sql.DB, dim int) (*SQLiteVecIndex, error) {
	// Verify sqlite-vec is available by checking for the vec_version function.
	var version string
	err := db.QueryRow("SELECT vec_version()").Scan(&version)
	if err != nil {
		return nil, fmt.Errorf("sqlite-vec not available: %w (install https://github.com/asg017/sqlite-vec)", err)
	}
	idx := &SQLiteVecIndex{db: db, dim: dim, loaded: true}
	return idx, nil
}

// Search returns the top-K entity IDs most similar to queryEmbedding
// using sqlite-vec's vector search.
func (idx *SQLiteVecIndex) Search(ctx context.Context, queryEmbedding []float32, limit int) ([]string, error) {
	if !idx.loaded {
		return nil, fmt.Errorf("sqlite-vec index not loaded")
	}
	// sqlite-vec stores vectors as BLOBs and provides vec_distance_cosine().
	// The actual SQL depends on the sqlite-vec version; this is a placeholder
	// for the architecture — the real implementation would use:
	//   SELECT id FROM entities WHERE archived = 0
	//   ORDER BY vec_distance_cosine(embedding, ?) ASC
	//   LIMIT ?
	_ = queryEmbedding
	_ = limit
	return nil, fmt.Errorf("sqlite-vec Search: not yet implemented — use in-memory backend")
}

// SearchBatch performs batch vector search via sqlite-vec.
func (idx *SQLiteVecIndex) SearchBatch(ctx context.Context, queries [][]float32, limit int) ([][]string, error) {
	if !idx.loaded {
		return nil, fmt.Errorf("sqlite-vec index not loaded")
	}
	out := make([][]string, len(queries))
	for i, q := range queries {
		ids, err := idx.Search(ctx, q, limit)
		if err != nil {
			return nil, err
		}
		out[i] = ids
	}
	return out, nil
}

// Store adds or updates a vector in the sqlite-vec index.
func (idx *SQLiteVecIndex) Store(_ context.Context, id string, vec []float32) error {
	if !idx.loaded {
		return fmt.Errorf("sqlite-vec index not loaded")
	}
	_ = id
	_ = vec
	return fmt.Errorf("sqlite-vec Store: not yet implemented")
}

// Remove deletes vectors from the sqlite-vec index.
func (idx *SQLiteVecIndex) Remove(_ context.Context, ids []string) error {
	if !idx.loaded {
		return fmt.Errorf("sqlite-vec index not loaded")
	}
	_ = ids
	return fmt.Errorf("sqlite-vec Remove: not yet implemented")
}
