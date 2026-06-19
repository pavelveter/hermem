package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type ExtractedEntity struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Content  string   `json:"content"`
	Relations []struct {
		TargetID     string `json:"target_id"`
		RelationType string `json:"relation_type"`
	} `json:"relations"`
}

type ExtractionResult struct {
	Entities []ExtractedEntity `json:"entities"`
}

type LLMExtractor interface {
	ExtractEntities(dialog string) (*ExtractionResult, error)
}

type IngestionWorker struct {
	db          *sql.DB
	extractor   LLMExtractor
	embedder    Embedder
	dedupThresh float32
}

func NewIngestionWorker(db *sql.DB, extractor LLMExtractor, embedder Embedder) *IngestionWorker {
	return &IngestionWorker{
		db:          db,
		extractor:   extractor,
		embedder:    embedder,
		dedupThresh: 0.88,
	}
}

func (w *IngestionWorker) ProcessDialog(dialog string) error {
	result, err := w.extractor.ExtractEntities(dialog)
	if err != nil {
		return fmt.Errorf("failed to extract entities: %w", err)
	}

	for _, entity := range result.Entities {
		if err := w.processEntity(entity); err != nil {
			return fmt.Errorf("failed to process entity %s: %w", entity.ID, err)
		}
	}

	return nil
}

func (w *IngestionWorker) processEntity(entity ExtractedEntity) error {
	embedding, err := w.embedder.Embed(entity.Content)
	if err != nil {
		return fmt.Errorf("failed to embed content: %w", err)
	}

	existing, err := w.findSimilarEntity(embedding)
	if err != nil {
		return fmt.Errorf("failed to find similar entity: %w", err)
	}

	entityID := entity.ID
	if existing != nil {
		entityID = existing.ID
		err = w.mergeEntities(existing, entity, embedding)
	} else {
		err = w.createEntity(entity, embedding)
	}

	if err != nil {
		return err
	}

	return w.createEdges(entityID, entity.Relations)
}

func (w *IngestionWorker) findSimilarEntity(embedding []float32) (*Entity, error) {
	rows, err := w.db.Query(`
		SELECT id, category, content, embedding, updated_at
		FROM entities
		WHERE embedding IS NOT NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bestMatch *Entity
	var bestSimilarity float32

	for rows.Next() {
		var entity Entity
		var embeddingBytes []byte

		if err := rows.Scan(&entity.ID, &entity.Category, &entity.Content, &embeddingBytes, &entity.UpdatedAt); err != nil {
			return nil, err
		}

		if len(embeddingBytes) == 0 {
			continue
		}

		entityEmbedding := BytesToEmbedding(embeddingBytes)
		if entityEmbedding == nil {
			continue
		}

		similarity := CosineSimilarity(embedding, entityEmbedding)
		if similarity > w.dedupThresh && similarity > bestSimilarity {
			bestMatch = &entity
			bestSimilarity = similarity
		}
	}

	return bestMatch, rows.Err()
}

func (w *IngestionWorker) mergeEntities(existing *Entity, newEntity ExtractedEntity, embedding []float32) error {
	mergedContent := existing.Content + " | " + newEntity.Content

	_, err := w.db.Exec(`
		UPDATE entities
		SET content = ?, embedding = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, mergedContent, EmbeddingToBytes(embedding), existing.ID)
	return err
}

func (w *IngestionWorker) createEntity(entity ExtractedEntity, embedding []float32) error {
	_, err := w.db.Exec(`
		INSERT OR REPLACE INTO entities (id, category, content, embedding, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, entity.ID, entity.Category, entity.Content, EmbeddingToBytes(embedding))
	return err
}

func (w *IngestionWorker) createEdges(entityID string, relations []struct {
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
}) error {
	for _, rel := range relations {
		_, err := w.db.Exec(`
			INSERT OR IGNORE INTO edges (source_id, target_id, relation_type)
			VALUES (?, ?, ?)
		`, entityID, rel.TargetID, rel.RelationType)
		if err != nil {
			return err
		}
	}
	return nil
}

type MemoryMessage struct {
	Dialog string
}

func MemoryWorker(db *sql.DB, extractor LLMExtractor, embedder Embedder, ch <-chan MemoryMessage) {
	worker := NewIngestionWorker(db, extractor, embedder)

	for msg := range ch {
		if err := worker.ProcessDialog(msg.Dialog); err != nil {
			fmt.Printf("Error processing dialog: %v\n", err)
		}
	}
}

type SimpleLLMExtractor struct {
	// In a real implementation, this would call an LLM API
}

func (e *SimpleLLMExtractor) ExtractEntities(dialog string) (*ExtractionResult, error) {
	result := &ExtractionResult{}

	lines := strings.Split(dialog, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		entity := ExtractedEntity{
			ID:       fmt.Sprintf("entity-%d", len(result.Entities)+1),
			Category: "world",
			Content:  line,
		}
		result.Entities = append(result.Entities, entity)
	}

	return result, nil
}

func ParseExtractionResult(data []byte) (*ExtractionResult, error) {
	var result ExtractionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
