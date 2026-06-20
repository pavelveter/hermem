package main

import (
	"database/sql"
	"fmt"
	"sort"
)

type SearchResult struct {
	Entity     Entity  `json:"entity"`
	Similarity float32 `json:"similarity"`
}

func SearchByVector(db *sql.DB, queryEmbedding []float32, topK int) ([]SearchResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("empty query embedding")
	}

	rows, err := db.Query(`
		SELECT id, category, content, embedding, updated_at 
		FROM entities 
		WHERE embedding IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query entities: %w", err)
	}
	defer rows.Close()

	var results []SearchResult

	for rows.Next() {
		var entity Entity
		var embeddingBytes []byte

		if err := rows.Scan(&entity.ID, &entity.Category, &entity.Content, &embeddingBytes, &entity.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan entity: %w", err)
		}

		if len(embeddingBytes) == 0 {
			continue
		}

		entityEmbedding := BytesToEmbedding(embeddingBytes)
		if entityEmbedding == nil {
			continue
		}

		similarity := CosineSimilarity(queryEmbedding, entityEmbedding)
		results = append(results, SearchResult{
			Entity:     entity,
			Similarity: similarity,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// Sort by similarity (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	// Return top K results
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

func AddEdgeWithAutoCreate(db *sql.DB, embedder Embedder, src, dst, rel string) error {
	for _, id := range []string{src, dst} {
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM entities WHERE id = ?)", id).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check entity %q: %w", id, err)
		}
		if !exists {
			embedding, err := embedder.Embed(id)
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

func AutoLinkEdges(db *sql.DB, embedder Embedder, newID string, newEmbedding []float32) error {
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
		_, err := db.Exec(`
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

func StoreEntityWithEmbedding(db *sql.DB, entity Entity) error {
	var embeddingBytes []byte
	if len(entity.Embedding) > 0 {
		embeddingBytes = EmbeddingToBytes(entity.Embedding)
	}

	_, err := db.Exec(`
		INSERT OR REPLACE INTO entities (id, category, content, embedding, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, entity.ID, entity.Category, entity.Content, embeddingBytes)
	if err != nil {
		return err
	}

	return nil
}
