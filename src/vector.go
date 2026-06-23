package main

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	embedDimOnce sync.Once
	embedDim     int

	metricsWorker *AsyncMetricsWorker
)

func InitMetricsWorker(db *sql.DB) *AsyncMetricsWorker {
	w := NewAsyncMetricsWorker(db, 5000, 100, 100*time.Millisecond)
	w.Start()
	metricsWorker = w
	return w
}

type SearchResult struct {
	Entity     Entity  `json:"entity"`
	Similarity float32 `json:"similarity"`
}

type VectorIndex interface {
	Search(ctx context.Context, vec []float32, limit int) ([]string, error)
	SearchBatch(ctx context.Context, vecs [][]float32, limit int) ([][]string, error)
	Store(ctx context.Context, id string, vec []float32) error
	Remove(ctx context.Context, ids []string) error
}

type vectorIndexFactory func(db *sql.DB, dim int) VectorIndex

var vectorIndexFactories = map[string]vectorIndexFactory{
	"in-memory": func(db *sql.DB, _ int) VectorIndex {
		return NewInMemoryVectorIndex(db)
	},
}

func newVectorIndex(backend string, db *sql.DB, dim int) VectorIndex {
	if f, ok := vectorIndexFactories[backend]; ok {
		return f(db, dim)
	}
	slog.Warn("vector backend not found, falling back to in-memory",
		"event", "vector_backend_fallback",
		"requested", backend,
	)
	return &InMemoryVectorIndex{db: db}
}

func SearchByVector(db *sql.DB, vi VectorIndex, queryEmbedding []float32, topK int) ([]SearchResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}

	// Clamp topK to the SQLite IN-clause ceiling. SQLite's default
	// SQLITE_MAX_VARIABLE_NUMBER is 999; the SELECT below feeds every
	// returned id into a single `IN (...)` placeholder list, so a
	// caller-supplied topK > 999 reproduces the same "too many SQL
	// variables" failure this PR fixes for writes. DefaultSQLBatchSize=500
	// leaves headroom against any future raise of the limit. Callers
	// can detect the clamp via `len(results) < topK`; we log debug so
	// operators can see it on demand.
	if topK > DefaultSQLBatchSize {
		slog.Debug("search_topk_clamped",
			"event", "search_topk_clamp",
			"requested", topK,
			"applied", DefaultSQLBatchSize,
		)
		topK = DefaultSQLBatchSize
	}

	ids, err := vi.Search(context.Background(), queryEmbedding, topK)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	phs, args := inClauseArgs(ids)

	rows, err := db.Query(fmt.Sprintf(`
		SELECT id, category, content, embedding, updated_at, last_accessed_at
		FROM entities WHERE id IN (%s) AND archived = 0
	`, phs), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch entities: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var entity Entity
		var embeddingBytes []byte
		var lastAccessed sql.NullTime
		if err := rows.Scan(&entity.ID, &entity.Category, &entity.Content, &embeddingBytes, &entity.UpdatedAt, &lastAccessed); err != nil {
			return nil, fmt.Errorf("failed to scan entity: %w", err)
		}
		if lastAccessed.Valid {
			entity.LastAccessedAt = &lastAccessed.Time
		}
		sim := float32(0)
		if len(embeddingBytes) > 0 {
			if emb, err := DecodeVector(embeddingBytes, len(queryEmbedding)); err == nil {
				sim = CosineSimilarity(queryEmbedding, emb)
			}
		}
		results = append(results, SearchResult{
			Entity:     entity,
			Similarity: sim,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating entities: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	if len(results) > 0 {
		for _, r := range results {
			metricsWorker.Touch(r.Entity.ID)
		}
	}
	return results, nil
}

func AddEdge(db *sql.DB, src, dst, rel string) error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM entities WHERE id IN (?, ?)", src, dst).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check entity existence: %w", err)
	}
	if count != 2 {
		return fmt.Errorf("both source and target entities must exist (found %d of 2)", count)
	}
	_, err = db.Exec(`
		INSERT OR IGNORE INTO edges (source_id, target_id, relation_type)
		VALUES (?, ?, ?)
	`, src, dst, rel)
	if err != nil {
		return fmt.Errorf("failed to insert edge: %w", err)
	}
	return nil
}

func AddEdgeWithAutoCreate(ctx context.Context, db *sql.DB, vi VectorIndex, embedder Embedder, src, dst, rel string) error {
	for _, id := range []string{src, dst} {
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM entities WHERE id = ?)", id).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check entity %q: %w", id, err)
		}
		if !exists {
			embedding, err := embedder.Embed(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to embed placeholder entity %q: %w", id, err)
			}
			if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
				ID:        id,
				Category:  "world",
				Content:   id,
				Embedding: embedding,
			}); err != nil {
				return fmt.Errorf("failed to store placeholder entity %q: %w", id, err)
			}
		}
	}
	return AddEdge(db, src, dst, rel)
}

func AutoLinkEdges(ctx context.Context, db *sql.DB, vi VectorIndex, embedder Embedder, newID string, newEmbedding []float32) error {
	if len(newEmbedding) == 0 {
		return fmt.Errorf("cannot auto-link: embedding is empty for %s", newID)
	}

	results, err := SearchByVector(db, vi, newEmbedding, 3)
	if err != nil {
		return fmt.Errorf("auto-link search: %w", err)
	}

	var inserted int
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
		_, err := db.ExecContext(ctx, `
			INSERT OR IGNORE INTO edges (source_id, target_id, relation_type)
			VALUES (?, ?, 'related_to')
		`, newID, r.Entity.ID)
		if err != nil {
			return fmt.Errorf("auto-link insert: %w", err)
		}
		inserted++
	}
	return nil
}

func checkEmbeddingDim(dim int) {
	embedDimOnce.Do(func() {
		embedDim = dim
		slog.Debug("embedding dimension set", "event", "embed_dim_set", "dim", dim)
	})
	if embedDim != 0 && dim != 0 && dim != embedDim {
		slog.Warn("embedding dimension mismatch",
			"event", "embed_dim_mismatch",
			"expected", embedDim,
			"got", dim,
		)
	}
}

// StoreEntityWithEmbedding persists entity to SQLite and mirrors its
// embedding into the vector index.
//
// Ordering: the vector index is updated BEFORE the SQLite commit so
// that, on concurrent ingest, the index can never lag the DB (a
// long-running DB write won't leave a hot index lagging behind
// committed DB rows). On SQLite failure, we roll back the index
// entry via vi.Remove. The worst-case race is:
//
//  1. vi.Store adds/replaces an entry for entity ID X.
//  2. SQLite INSERT OR REPLACE fails (constraint, lock timeout).
//  3. vi.Remove removes the index entry.
//
// For an *update* of an existing row, step 3 means the prior index
// entry is lost along with the (also lost) new entry; the DB still
// holds the old data. SearchByVector simply returns one fewer result
// for that ID, and the caller can retry. Net effect: index never
// points to a row the DB doesn't have, and SQLite errors don't leave
// phantom index entries pointing at non-existent IDs.
//
// Schema parameter: replaces the previous package-level activeSchema
// global. The status-default rule (stateful category → first
// valid_state) is read here so callers that store without setting a
// status get the documented default without touching global state.
func StoreEntityWithEmbedding(db *sql.DB, vi VectorIndex, schema SchemaConfig, entity Entity) error {
	var embeddingBytes []byte
	hasEmbedding := len(entity.Embedding) > 0
	if hasEmbedding {
		NormalizeVector(entity.Embedding)
		checkEmbeddingDim(len(entity.Embedding))
		embeddingBytes = EmbeddingToBytes(entity.Embedding)
	}

	if hasEmbedding {
		if err := vi.Store(context.Background(), entity.ID, entity.Embedding); err != nil {
			return fmt.Errorf("vector index store: %w", err)
		}
	}

	status := entity.Status
	if status == "" && schema.StatefulCategories[entity.Category] && len(schema.ValidStateOrder) > 0 {
		status = schema.ValidStateOrder[0]
	}
	_, err := db.Exec(`
		INSERT OR REPLACE INTO entities (id, category, content, embedding, updated_at, status)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?)
	`, entity.ID, entity.Category, entity.Content, embeddingBytes, nullString(status))
	if err != nil {
		if hasEmbedding {
			if rmErr := vi.Remove(context.Background(), []string{entity.ID}); rmErr != nil {
				slog.Warn("vector index rollback after sqlite failure",
					"event", "vector_rollback_fail",
					"entity_id", entity.ID,
					"rm_err", rmErr,
				)
			}
		}
		return err
	}
	return nil
}

func nullString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

// orNullTime returns nil for nil *time.Time, otherwise the underlying value.
// Used in INSERT statements so NULL propagates correctly for optional timestamps.
func orNullTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}

// inClauseArgs builds N "?" placeholders and an args slice for SQL
// IN (...) queries. Avoids raw string concatenation of SQL fragments.
func inClauseArgs(ids []string) (string, []interface{}) {
	phs := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		phs[i] = "?"
		args[i] = id
	}
	return strings.Join(phs, ","), args
}

func EmbeddingToBytes(embedding []float32) []byte {
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func BytesToEmbedding(data []byte) []float32 {
	if len(data)%4 != 0 {
		return nil
	}
	embedding := make([]float32, len(data)/4)
	for i := range embedding {
		bits := binary.LittleEndian.Uint32(data[i*4 : i*4+4])
		embedding[i] = math.Float32frombits(bits)
	}
	return embedding
}
