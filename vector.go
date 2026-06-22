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
)

var (
	embedDimOnce sync.Once
	embedDim     int

	currentVectorIndex VectorIndex = &InMemoryVectorIndex{}
)

type SearchResult struct {
	Entity     Entity  `json:"entity"`
	Similarity float32 `json:"similarity"`
}

type VectorIndex interface {
	Search(ctx context.Context, vec []float32, limit int) ([]string, error)
	SearchBatch(ctx context.Context, vecs [][]float32, limit int) ([][]string, error)
	Store(ctx context.Context, id string, vec []float32) error
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

func SearchByVector(db *sql.DB, queryEmbedding []float32, topK int) ([]SearchResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}

	ids, err := currentVectorIndex.Search(context.Background(), queryEmbedding, topK)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := db.Query(fmt.Sprintf(`
		SELECT id, category, content, embedding, updated_at
		FROM entities WHERE id IN (%s)
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch entities: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var entity Entity
		var embeddingBytes []byte
		if err := rows.Scan(&entity.ID, &entity.Category, &entity.Content, &embeddingBytes, &entity.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan entity: %w", err)
		}
		sim := float32(0)
		if len(embeddingBytes) > 0 {
			if emb := BytesToEmbedding(embeddingBytes); emb != nil {
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

func AddEdgeWithAutoCreate(ctx context.Context, db *sql.DB, embedder Embedder, src, dst, rel string) error {
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
			if err := StoreEntityWithEmbedding(db, Entity{
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

func AutoLinkEdges(ctx context.Context, db *sql.DB, embedder Embedder, newID string, newEmbedding []float32) error {
	if len(newEmbedding) == 0 {
		return fmt.Errorf("cannot auto-link: embedding is empty for %s", newID)
	}

	results, err := SearchByVector(db, newEmbedding, 3)
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

func StoreEntityWithEmbedding(db *sql.DB, entity Entity) error {
	var embeddingBytes []byte
	if len(entity.Embedding) > 0 {
		checkEmbeddingDim(len(entity.Embedding))
		embeddingBytes = EmbeddingToBytes(entity.Embedding)
	}
	_, err := db.Exec(`
		INSERT OR REPLACE INTO entities (id, category, content, embedding, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, entity.ID, entity.Category, entity.Content, embeddingBytes)
	if err != nil {
		return err
	}
	if len(entity.Embedding) > 0 {
		return currentVectorIndex.Store(context.Background(), entity.ID, entity.Embedding)
	}
	return nil
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


