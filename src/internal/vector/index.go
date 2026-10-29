package vector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

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
	if topK <= 0 {
		return nil, nil
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
	// unixepoch() keeps the driver from parsing the DATETIME text per row:
	// every writer stores second-resolution CURRENT_TIMESTAMP, so the integer
	// form is lossless and time.Parse was ~8% of this call's CPU.
	rows, err := db.Query(`SELECT id, category, content, unixepoch(updated_at), unixepoch(last_accessed_at) FROM entities WHERE id IN (`+phs+`) AND archived = 0`, args...)
	if err != nil {
		return nil, fmt.Errorf("fetch entities: %w", err)
	}
	defer rows.Close()
	byID := make(map[string]domain.Entity, len(ids))
	for rows.Next() {
		var e domain.Entity
		var updatedAtUnix, lastAccUnix sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &updatedAtUnix, &lastAccUnix); err != nil {
			return nil, fmt.Errorf("scan entity: %w", err)
		}
		if updatedAtUnix.Valid {
			t := time.Unix(updatedAtUnix.Int64, 0).UTC()
			e.UpdatedAt = &t
		}
		if lastAccUnix.Valid {
			t := time.Unix(lastAccUnix.Int64, 0).UTC()
			e.LastAccessedAt = &t
		}
		byID[e.ID] = e
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fetch entities rows: %w", err)
	}
	// Hydration via `WHERE id IN (...)` returns rows in storage order, so the
	// ranking is taken from the vector store's hits, which are already ordered
	// by descending similarity. Archived or vanished rows are simply dropped.
	results := make([]domain.SearchResult, 0, len(byID))
	for _, hit := range hits {
		e, ok := byID[hit.ID]
		if !ok {
			continue
		}
		results = append(results, domain.SearchResult{Entity: e, Similarity: hit.Score})
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
