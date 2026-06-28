package vector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// Sentinel errors returned by VectorIndex implementations when the
// supplied query dimension or the in-memory matrix layout is inconsistent
// with the index's contracted shape. Callers MUST check for these via
// errors.Is before any other recovery; the in-function bounds-bump panic
// in BatchDotProducts remains as a last-line defensive check.
var (
	ErrInvalidQueryDim = errors.New("vector: query dimension mismatch with index")
	ErrMatrixCorrupted = errors.New("vector: flat matrix size != N * dim")
)

// NewIndex creates a VectorIndex for the given backend.
// Currently supports:
//   - "in-memory" (default): brute-force cosine similarity in memory
//   - "sqlite-vec": sqlite-vec extension (requires the extension to be loaded)
//
// Unknown backends fall back to in-memory with a warning logged by the caller.
func NewIndex(backend string, db *sql.DB, dim int) core.VectorIndex {
	switch backend {
	case "sqlite-vec":
		idx, err := NewSQLiteVecIndex(db, dim)
		if err != nil {
			// Fall back to in-memory if sqlite-vec is not available.
			return NewInMemoryVectorIndex(db)
		}
		return idx
	default: // "in-memory" or ""
		return NewInMemoryVectorIndex(db)
	}
}

// SearchByVector finds the topK entities most similar to queryEmbedding and hydrates from DB.
func SearchByVector(db *sql.DB, vi core.VectorIndex, queryEmbedding []float32, topK int) ([]core.SearchResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}
	if topK > 500 {
		topK = 500
	}
	ids, err := vi.Search(context.Background(), queryEmbedding, topK)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	phs, args := store.InClauseArgs(ids)
	rows, err := db.Query(fmt.Sprintf(`SELECT id, category, content, embedding, updated_at, last_accessed_at FROM entities WHERE id IN (%s) AND archived = 0`, phs), args...)
	if err != nil {
		return nil, fmt.Errorf("fetch entities: %w", err)
	}
	defer rows.Close()
	var results []core.SearchResult
	for rows.Next() {
		var e core.Entity
		var embBytes []byte
		var lastAcc sql.NullTime
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &embBytes, &e.UpdatedAt, &lastAcc); err != nil {
			return nil, fmt.Errorf("scan entity: %w", err)
		}
		if lastAcc.Valid {
			e.LastAccessedAt = &lastAcc.Time
		}
		sim := float32(0)
		if len(embBytes) > 0 {
			if emb, err := store.DecodeVector(embBytes, len(queryEmbedding)); err == nil {
				sim = CosineSimilarity(queryEmbedding, emb)
			}
		}
		results = append(results, core.SearchResult{Entity: e, Similarity: sim})
	}
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

// AddEdgeWithAutoCreate creates an edge, auto-creating missing entities with id-as-content placeholder embeddings.
func AddEdgeWithAutoCreate(ctx context.Context, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, src, dst, rel string) error {
	for _, id := range []string{src, dst} {
		var exists bool
		if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM entities WHERE id = ?)", id).Scan(&exists); err != nil {
			return fmt.Errorf("check %q: %w", id, err)
		}
		if !exists {
			embedding, err := embedder.Embed(ctx, id)
			if err != nil {
				return fmt.Errorf("embed placeholder %q: %w", id, err)
			}
			if err := store.StoreEntityWithEmbedding(ctx, db, vi, core.DefaultSchemaConfig(false), core.Entity{
				ID: id, Category: "world", Content: id, Embedding: embedding,
			}); err != nil {
				return fmt.Errorf("store placeholder %q: %w", id, err)
			}
		}
	}
	return store.AddEdge(db, src, dst, rel, 1.0)
}

// AutoLinkEdges links a new entity to its top-3 closest neighbors with similarity > 0.85.
func AutoLinkEdges(ctx context.Context, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, newID string, newEmbedding []float32) error {
	if len(newEmbedding) == 0 {
		return fmt.Errorf("empty embedding for %s", newID)
	}
	results, err := SearchByVector(db, vi, newEmbedding, 3)
	if err != nil {
		return fmt.Errorf("auto-link search: %w", err)
	}
	inserted := 0
	for _, r := range results {
		if inserted >= 3 {
			break
		}
		if r.Entity.ID == newID {
			continue
		}
		if r.Similarity <= 0.85 {
			continue
		}
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, 'related_to', 1.0)`, newID, r.Entity.ID); err != nil {
			return fmt.Errorf("auto-link insert: %w", err)
		}
		inserted++
	}
	return nil
}
