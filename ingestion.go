package main

import (
	"database/sql"
	"fmt"
	"strings"
)

type ExtractedEntity struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	Content   string `json:"content"`
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

	targetID := entity.ID
	if existing != nil {
		targetID = existing.ID
		err = w.mergeEntities(existing, entity, embedding)
	} else {
		err = w.createEntity(entity, embedding)
	}

	if err != nil {
		return err
	}

	return w.createEdges(targetID, entity.Relations)
}

func (w *IngestionWorker) findSimilarEntity(embedding []float32) (*Entity, error) {
	results, err := SearchByVector(w.db, embedding, 1)
	if err != nil {
		return nil, err
	}

	if len(results) > 0 && results[0].Similarity >= w.dedupThresh {
		return &results[0].Entity, nil
	}
	return nil, nil
}

func (w *IngestionWorker) createEntity(entity ExtractedEntity, embedding []float32) error {
	dbEntity := Entity{
		ID:        entity.ID,
		Category:  entity.Category,
		Content:   entity.Content,
		Embedding: embedding,
	}
	return StoreEntityWithEmbedding(w.db, dbEntity)
}

func (w *IngestionWorker) mergeEntities(existing *Entity, newEntity ExtractedEntity, newEmbedding []float32) error {
	mergedContent := existing.Content
	if !strings.Contains(existing.Content, newEntity.Content) {
		mergedContent = existing.Content + "; " + newEntity.Content
	}

	updatedEmbedding, err := w.embedder.Embed(mergedContent)
	if err != nil {
		return fmt.Errorf("failed to re-embed merged content: %w", err)
	}

	existing.Content = mergedContent
	existing.Embedding = updatedEmbedding

	return StoreEntityWithEmbedding(w.db, *existing)
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
			return fmt.Errorf("failed to insert edge %s -> %s: %w", entityID, rel.TargetID, err)
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
			fmt.Printf("Error processing dialog in background: %v\n", err)
		}
	}
}

type SimpleLLMExtractor struct{}

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
