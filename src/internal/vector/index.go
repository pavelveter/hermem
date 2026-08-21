package vector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/pavelveter/hermem/pkg/domain"
	"github.com/pavelveter/hermem/pkg/spi"
	"github.com/pavelveter/hermem/src/internal/store"
)

// maxSearchLimit caps the per-call result count accepted by searches over
// user-controlled limits. Callers MUST validate against this before any
// allocation sized by a user-controlled limit; the CodeQL
// `go/uncontrolled-allocation-size` rule conservatively flags
// `make([]T, limit)` where `limit` is a function parameter.
//
// SearchByVector's `if topK > maxSearchLimit { topK = maxSearchLimit }`
// reads from this constant. Suite-wide consumers (e.g.
// retrieval/service.go's DefaultSearchTopK) keep independent defaults
// and are NOT auto-raised when this constant changes — audit those
// call-sites if you bump it.
const maxSearchLimit = 500

// MaxResultsCap is the cross-package exported alias of maxSearchLimit.
// Other packages (notably src/internal/retrieval) should reference this
// alias so their per-consumer defaults stay correctly anchored to the
// cap if the underlying value is ever raised. The alias intentionally
// keeps the unexported `maxSearchLimit` as the security-validation source
// of truth inside vector/, since only this package stores the data on
// which the cap matters.
const MaxResultsCap = maxSearchLimit

// ErrLimitOutOfRange is returned by VectorStore implementations when the
// caller-supplied search limit is negative (the public-contract stores
// have no upper bound; SearchByVector clamps topK to MaxResultsCap instead).
var ErrLimitOutOfRange = errors.New("vector: search limit out of range")

// SearchByVector finds the topK entities most similar to queryEmbedding and hydrates from DB.
func SearchByVector(ctx context.Context, db *sql.DB, vi spi.VectorStore, queryEmbedding []float32, topK int) ([]domain.SearchResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}
	if topK > maxSearchLimit {
		topK = maxSearchLimit
	}
	hits, err := vi.Search(ctx, spi.SearchRequest{Namespace: spi.DefaultNamespace, Vector: queryEmbedding, Limit: topK})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		ids = append(ids, hit.ID)
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
	var results []domain.SearchResult
	for rows.Next() {
		var e domain.Entity
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
		results = append(results, domain.SearchResult{Entity: e, Similarity: sim})
	}
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

// AddEdgeWithAutoCreate creates an edge, auto-creating missing entities with id-as-content placeholder embeddings.
func AddEdgeWithAutoCreate(ctx context.Context, db *sql.DB, vi spi.VectorStore, embedder spi.Embedder, src, dst, rel string) error {
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
			if err := store.StoreEntityWithEmbedding(ctx, db, vi, domain.DefaultSchemaConfig(false), domain.Entity{
				ID: id, Category: "world", Content: id, Embedding: embedding,
			}); err != nil {
				return fmt.Errorf("store placeholder %q: %w", id, err)
			}
		}
	}
	return store.AddEdge(ctx, db, src, dst, rel, 1.0)
}

// AutoLinkEdges links a new entity to its top-3 closest neighbors with similarity > 0.85.
func AutoLinkEdges(ctx context.Context, db *sql.DB, vi spi.VectorStore, embedder spi.Embedder, newID string, newEmbedding []float32) error {
	if len(newEmbedding) == 0 {
		return fmt.Errorf("empty embedding for %s", newID)
	}
	results, err := SearchByVector(ctx, db, vi, newEmbedding, 3)
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
