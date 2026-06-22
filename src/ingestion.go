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
	vi          VectorIndex
	extractor   LLMExtractor
	embedder    Embedder
	dedupThresh float32
}

func NewIngestionWorker(db *sql.DB, vi VectorIndex, extractor LLMExtractor, embedder Embedder, dedupThreshold float32) *IngestionWorker {
	return &IngestionWorker{
		db:          db,
		vi:          vi,
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

	type item struct {
		entity    ExtractedEntity
		embedding []float32
	}
	items := make([]item, 0, len(result.Entities))

	for _, entity := range result.Entities {
		embedding, err := w.embedder.Embed(ctx, entity.Content)
		if err != nil {
			slog.Error("entity processing failed, continuing",
				"event", "entity_failed",
				"entity_id", entity.ID,
				"err", err,
			)
			continue
		}
		items = append(items, item{entity: entity, embedding: embedding})
	}

	if len(items) == 0 {
		return nil
	}

	embeddings := make([][]float32, len(items))
	for i, it := range items {
		embeddings[i] = it.embedding
	}

	allIDs, err := w.vi.SearchBatch(ctx, embeddings, 1)
	if err != nil {
		return fmt.Errorf("batch similar search failed: %w", err)
	}

	for i, it := range items {
		targetID := it.entity.ID
		existing, err := w.findMatch(it.embedding, allIDs[i], it.entity.ID)
		if err != nil {
			slog.Error("entity processing failed, continuing",
				"event", "entity_failed",
				"entity_id", it.entity.ID,
				"err", err,
			)
			continue
		}

		if existing != nil {
			targetID = existing.ID
			err = w.mergeEntities(ctx, existing, it.entity, it.embedding)
		} else {
			err = w.createEntity(it.entity, it.embedding)
		}
		if err != nil {
			slog.Error("entity processing failed, continuing",
				"event", "entity_failed",
				"entity_id", it.entity.ID,
				"err", err,
			)
			continue
		}

		if err := w.createEdges(targetID, it.entity.Relations); err != nil {
			slog.Error("entity processing failed, continuing",
				"event", "entity_failed",
				"entity_id", it.entity.ID,
				"err", err,
			)
		}
	}
	return nil
}

func (w *IngestionWorker) findMatch(embedding []float32, similarIDs []string, selfID string) (*Entity, error) {
	if len(similarIDs) == 0 {
		return nil, nil
	}

	// If the top match is the entity itself (from a re-embed), skip it
	candidateID := similarIDs[0]
	if candidateID == selfID {
		if len(similarIDs) < 2 {
			return nil, nil
		}
		candidateID = similarIDs[1]
	}

	var entity Entity
	var embeddingBytes []byte
	err := w.db.QueryRow(
		`SELECT id, category, content, embedding, updated_at FROM entities WHERE id = ?`,
		candidateID,
	).Scan(&entity.ID, &entity.Category, &entity.Content, &embeddingBytes, &entity.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to fetch candidate %q: %w", candidateID, err)
	}

	sim := float32(0)
	if len(embeddingBytes) > 0 {
		if emb, err := DecodeVector(embeddingBytes, len(embedding)); err == nil {
			sim = CosineSimilarity(embedding, emb)
		}
	}

	if sim >= w.dedupThresh {
		return &entity, nil
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
	return StoreEntityWithEmbedding(w.db, w.vi, dbEntity)
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

	return StoreEntityWithEmbedding(w.db, w.vi, *existing)
}

// createEdges inserts all relations for entityID in bounded chunks
// of multi-VALUES INSERTs. Each edge consumes 3 host parameters
// (source_id, target_id, relation_type). edgesPerChunk is derived
// from DefaultSQLBatchSize / 3 so that adding a schema column
// (weight, updated_at, ...) on this table will automatically shrink
// the per-edge parameter count and retune the ceiling without a
// code edit here.
//
// Behaviour preserved: empty input short-circuits to nil; on the
// first chunk that fails the function returns that chunk's error
// immediately (matching the previous per-edge abort semantics).
// INSERT OR IGNORE continues to collapse duplicate
// (source, target, relation_type) tuples silently.
func (w *IngestionWorker) createEdges(entityID string, relations []Relation) error {
	if len(relations) == 0 {
		return nil
	}

	const edgesPerChunk = DefaultSQLBatchSize / 3

	for start := 0; start < len(relations); start += edgesPerChunk {
		end := start + edgesPerChunk
		if end > len(relations) {
			end = len(relations)
		}
		chunk := relations[start:end]

		args := make([]interface{}, 0, len(chunk)*3)
		phs := make([]string, len(chunk))
		for i, rel := range chunk {
			args = append(args, entityID, rel.TargetID, rel.RelationType)
			phs[i] = "(?, ?, ?)"
		}
		q := `INSERT OR IGNORE INTO edges (source_id, target_id, relation_type) VALUES ` +
			strings.Join(phs, ",")
		if _, err := w.db.Exec(q, args...); err != nil {
			return fmt.Errorf("bulk insert edges for %s: chunk [%d-%d] of %d: %w",
				entityID, start, end, len(relations), err)
		}
	}
	return nil
}

type MemoryMessage struct {
	Dialog string
}

func MemoryWorker(ctx context.Context, db *sql.DB, vi VectorIndex, extractor LLMExtractor, embedder Embedder, dedupThreshold float32, ch <-chan MemoryMessage) {
	worker := NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold)
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
