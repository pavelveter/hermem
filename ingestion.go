package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

type IngestionWorker struct {
	db          *sql.DB
	extractor   LLMExtractor
	embedder    Embedder
	dedupThresh float32
}

// NewIngestionWorker builds a worker that dedups incoming entities
// against existing ones when their cosine similarity is >= dedupThreshold.
// The threshold is owned by Config so it can be tuned per deployment.
func NewIngestionWorker(db *sql.DB, extractor LLMExtractor, embedder Embedder, dedupThreshold float32) *IngestionWorker {
	return &IngestionWorker{
		db:          db,
		extractor:   extractor,
		embedder:    embedder,
		dedupThresh: dedupThreshold,
	}
}

func (w *IngestionWorker) ProcessDialog(ctx context.Context, dialog string) error {
	result, err := w.extractor.ExtractEntities(ctx, dialog)
	if err != nil {
		return fmt.Errorf("failed to extract entities: %w", err)
	}

	for _, entity := range result.Entities {
		if err := w.processEntity(ctx, entity); err != nil {
			return fmt.Errorf("failed to process entity %s: %w", entity.ID, err)
		}
	}
	return nil
}

func (w *IngestionWorker) processEntity(ctx context.Context, entity ExtractedEntity) error {
	embedding, err := w.embedder.Embed(ctx, entity.Content)
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
		err = w.mergeEntities(ctx, existing, entity, embedding)
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

func (w *IngestionWorker) mergeEntities(ctx context.Context, existing *Entity, newEntity ExtractedEntity, newEmbedding []float32) error {
	mergedContent := existing.Content
	if !strings.Contains(existing.Content, newEntity.Content) {
		mergedContent = existing.Content + "; " + newEntity.Content
	}

	updatedEmbedding, err := w.embedder.Embed(ctx, mergedContent)
	if err != nil {
		return fmt.Errorf("failed to re-embed merged content: %w", err)
	}

	existing.Content = mergedContent
	existing.Embedding = updatedEmbedding

	return StoreEntityWithEmbedding(w.db, *existing)
}

func (w *IngestionWorker) createEdges(entityID string, relations []Relation) error {
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

func MemoryWorker(ctx context.Context, db *sql.DB, extractor LLMExtractor, embedder Embedder, dedupThreshold float32, ch <-chan MemoryMessage) {
	worker := NewIngestionWorker(db, extractor, embedder, dedupThreshold)
	for msg := range ch {
		if err := worker.ProcessDialog(ctx, msg.Dialog); err != nil {
			slog.Error("dialog processing failed",
				"event", "ingest_failed",
				"err", err,
				"dialog_len", len(msg.Dialog),
			)
		}
	}
}

type SimpleLLMExtractor struct{}

func (e *SimpleLLMExtractor) ExtractEntities(ctx context.Context, dialog string) (*ExtractionResult, error) {
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
