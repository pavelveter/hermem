// Package ingestion provides the dialog-to-entity ingestion pipeline.
package ingestion

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// IngestionWorker handles the extraction→embed→store pipeline.
type IngestionWorker struct {
	db          *sql.DB
	vi          core.VectorIndex
	extractor   core.LLMExtractor
	embedder    core.Embedder
	dedupThresh float32
	schema      core.SchemaConfig
}

// NewIngestionWorker creates a worker.
func NewIngestionWorker(db *sql.DB, vi core.VectorIndex, extractor core.LLMExtractor, embedder core.Embedder, dedupThreshold float32, schema core.SchemaConfig) *IngestionWorker {
	return &IngestionWorker{db: db, vi: vi, extractor: extractor, embedder: embedder, dedupThresh: dedupThreshold, schema: schema}
}

// ReloadSchema swaps the schema on SIGHUP.
func (w *IngestionWorker) ReloadSchema(schema core.SchemaConfig) { w.schema = schema }

// ProcessDialog is the backward-compatible entry point.
func (w *IngestionWorker) ProcessDialog(ctx context.Context, dialog string) error {
	return w.ProcessDialogWithProvenance(ctx, dialog, core.Provenance{ExtractedFrom: dialog})
}

// ProcessDialogWithProvenance loads, embeds, and stores entities from one dialog.
func (w *IngestionWorker) ProcessDialogWithProvenance(ctx context.Context, dialog string, prov core.Provenance) error {
	result, err := w.extractor.ExtractEntities(ctx, dialog)
	if err != nil {
		return fmt.Errorf("extract entities: %w", err)
	}
	type item struct {
		entity    core.ExtractedEntity
		embedding []float32
	}
	items := make([]item, 0, len(result.Entities))
	for _, entity := range result.Entities {
		embedding, err := w.embedder.Embed(ctx, entity.Content)
		if err != nil {
			slog.Error("entity embed failed", "entity_id", entity.ID, "err", err)
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
		return fmt.Errorf("batch search: %w", err)
	}

	var bulkPairs []core.BulkPair
	for _, it := range items {
		vector.NormalizeVector(it.embedding)
		bulkPairs = append(bulkPairs, core.BulkPair{ID: it.entity.ID, Vec: it.embedding})
	}
	if err := w.vi.BulkStore(ctx, bulkPairs); err != nil {
		return fmt.Errorf("bulk store: %w", err)
	}

	for i, it := range items {
		targetID := it.entity.ID
		existing, err := w.findMatch(it.embedding, allIDs[i], it.entity.ID)
		if err != nil {
			slog.Error("entity match failed", "entity_id", it.entity.ID, "err", err)
			continue
		}

		if existing != nil && isContradiction(existing.Content, it.entity.Content) {
			existingConf := existing.Confidence
			if existingConf == 0 {
				existingConf = 1.0
			}
			if existingConf >= 0.7 {
				slog.Info("contradiction detected, keeping both", "existing_id", existing.ID, "incoming_id", it.entity.ID)
				it.entity.Relations = append(it.entity.Relations, core.Relation{TargetID: existing.ID, RelationType: "contradicts"})
				existing = nil
			} else {
				slog.Info("contradiction resolved: preferring incoming", "existing_id", existing.ID, "incoming_id", it.entity.ID)
				w.db.ExecContext(ctx, `UPDATE entities SET archived = 1 WHERE id = ?`, existing.ID)
				w.vi.Remove(ctx, []string{existing.ID})
				existing = nil
			}
		}

		var mergeEntity *core.Entity
		if existing != nil {
			targetID = existing.ID
			mergedContent := existing.Content
			if !strings.Contains(existing.Content, it.entity.Content) {
				mergedContent = existing.Content + "; " + it.entity.Content
			}
			updatedEmb, embErr := w.embedder.Embed(ctx, mergedContent)
			if embErr != nil {
				slog.Error("merge embed failed", "entity_id", it.entity.ID, "err", embErr)
				continue
			}
			vector.NormalizeVector(updatedEmb)
			now := time.Now()
			mergeEntity = &core.Entity{
				ID: existing.ID, Category: existing.Category, Content: mergedContent,
				Embedding: updatedEmb, Status: existing.Status,
				CreatedAt: existing.CreatedAt, Confidence: 1.0,
				ConversationID: prov.ConversationID, MessageID: prov.MessageID,
				ExtractedFrom: prov.ExtractedFrom, Source: "dialog", SourceType: "extraction",
				UpdatedAt: now,
			}
			w.vi.Remove(ctx, []string{it.entity.ID})
		}

		itemTx, err := w.db.BeginTx(ctx, nil)
		if err != nil {
			if mergeEntity == nil {
				w.vi.Remove(ctx, []string{it.entity.ID})
			}
			slog.Error("begin tx failed", "entity_id", it.entity.ID, "err", err)
			continue
		}
		var writeErr error
		if mergeEntity != nil {
			writeErr = w.mergeEntityInTx(ctx, itemTx, *mergeEntity)
		} else {
			writeErr = w.createEntityInTx(ctx, itemTx, it.entity, it.embedding, prov)
		}
		if writeErr == nil {
			writeErr = w.createEdgesInTx(ctx, itemTx, targetID, it.entity.Relations)
		}
		rollbackID := it.entity.ID
		if mergeEntity != nil {
			rollbackID = ""
		}
		if writeErr != nil {
			itemTx.Rollback()
			if rollbackID != "" {
				w.vi.Remove(ctx, []string{rollbackID})
			}
			slog.Error("entity write failed", "entity_id", it.entity.ID, "err", writeErr)
			continue
		}
		if err := itemTx.Commit(); err != nil {
			if rollbackID != "" {
				w.vi.Remove(ctx, []string{rollbackID})
			}
			slog.Error("commit failed", "entity_id", it.entity.ID, "err", err)
			continue
		}
		if mergeEntity != nil {
			w.vi.Store(ctx, mergeEntity.ID, mergeEntity.Embedding)
		}
	}
	return nil
}

func (w *IngestionWorker) mergeEntityInTx(ctx context.Context, tx *sql.Tx, e core.Entity) error {
	embBytes := store.EmbeddingToBytes(e.Embedding)
	status := e.Status
	if status == "" && w.schema.StatefulCategories[e.Category] && len(w.schema.ValidStateOrder) > 0 {
		status = w.schema.ValidStateOrder[0]
	}
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO entities (id, category, content, embedding, updated_at, status, confidence, source, source_type, created_at, conversation_id, message_id, extracted_from) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Category, e.Content, embBytes, store.NullString(status), e.Confidence, e.Source, e.SourceType, store.OrNullTime(e.CreatedAt), e.ConversationID, e.MessageID, e.ExtractedFrom)
	if err != nil {
		return fmt.Errorf("merge entity: %w", err)
	}
	return nil
}

func (w *IngestionWorker) createEntityInTx(ctx context.Context, tx *sql.Tx, entity core.ExtractedEntity, embedding []float32, prov core.Provenance) error {
	vector.NormalizeVector(embedding)
	dbEntity := core.Entity{
		ID: entity.ID, Category: entity.Category, Content: entity.Content,
		Embedding: embedding, Confidence: 1.0, Source: "dialog", SourceType: "extraction",
		ConversationID: prov.ConversationID, MessageID: prov.MessageID, ExtractedFrom: prov.ExtractedFrom,
	}
	embBytes := store.EmbeddingToBytes(dbEntity.Embedding)
	status := dbEntity.Status
	if status == "" && w.schema.StatefulCategories[dbEntity.Category] && len(w.schema.ValidStateOrder) > 0 {
		status = w.schema.ValidStateOrder[0]
	}
	_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO entities (id, category, content, embedding, updated_at, status, confidence, source, source_type, created_at, conversation_id, message_id, extracted_from) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?, ?, ?)`,
		dbEntity.ID, dbEntity.Category, dbEntity.Content, embBytes, store.NullString(status), dbEntity.Confidence, dbEntity.Source, dbEntity.SourceType, dbEntity.ConversationID, dbEntity.MessageID, dbEntity.ExtractedFrom)
	if err != nil {
		return fmt.Errorf("insert entity: %w", err)
	}
	return nil
}

func (w *IngestionWorker) createEdgesInTx(ctx context.Context, tx *sql.Tx, entityID string, relations []core.Relation) error {
	if len(relations) == 0 {
		return nil
	}
	const chunkSize = 166
	for start := 0; start < len(relations); start += chunkSize {
		end := start + chunkSize
		if end > len(relations) {
			end = len(relations)
		}
		chunk := relations[start:end]
		args := make([]interface{}, 0, len(chunk)*4)
		phs := make([]string, len(chunk))
		for i, rel := range chunk {
			if !w.schema.AllowedRelations[rel.RelationType] {
				return fmt.Errorf("unknown relation_type: %s", rel.RelationType)
			}
			args = append(args, entityID, rel.TargetID, rel.RelationType, 1.0)
			phs[i] = "(?, ?, ?, ?)"
		}
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO edges (source_id, target_id, relation_type, weight) VALUES `+strings.Join(phs, ","), args...)
		if err != nil {
			return fmt.Errorf("bulk insert edges: %w", err)
		}
	}
	return nil
}

func (w *IngestionWorker) findMatch(embedding []float32, similarIDs []string, selfID string) (*core.Entity, error) {
	if len(similarIDs) == 0 {
		return nil, nil
	}
	candidateID := similarIDs[0]
	if candidateID == selfID {
		if len(similarIDs) < 2 {
			return nil, nil
		}
		candidateID = similarIDs[1]
	}
	var entity core.Entity
	var embBytes []byte
	var confidence sql.NullFloat64
	var source, sourceType, convID, msgID, extrFrom sql.NullString
	var createdAt sql.NullTime
	err := w.db.QueryRow(`SELECT id, category, content, embedding, updated_at, confidence, source, source_type, created_at, conversation_id, message_id, extracted_from FROM entities WHERE id = ?`, candidateID).Scan(
		&entity.ID, &entity.Category, &entity.Content, &embBytes, &entity.UpdatedAt, &confidence, &source, &sourceType, &createdAt, &convID, &msgID, &extrFrom)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch candidate %q: %w", candidateID, err)
	}
	if confidence.Valid {
		entity.Confidence = float32(confidence.Float64)
	}
	if source.Valid {
		entity.Source = source.String
	}
	if sourceType.Valid {
		entity.SourceType = sourceType.String
	}
	if createdAt.Valid {
		t := createdAt.Time
		entity.CreatedAt = &t
	}
	if convID.Valid {
		entity.ConversationID = convID.String
	}
	if msgID.Valid {
		entity.MessageID = msgID.String
	}
	if extrFrom.Valid {
		entity.ExtractedFrom = extrFrom.String
	}
	sim := float32(0)
	if len(embBytes) > 0 {
		if emb, err := store.DecodeVector(embBytes, len(embedding)); err == nil {
			sim = vector.CosineSimilarity(embedding, emb)
		}
	}
	if sim >= w.dedupThresh {
		return &entity, nil
	}
	return nil, nil
}

// isContradiction is a simple heuristic: negation words in one but not the other.
func isContradiction(a, b string) bool {
	negWords := []string{"not", "don't", "doesn't", "isn't", "aren't", "won't", "can't", "never", "no", "hate", "dislike"}
	al := strings.ToLower(a)
	bl := strings.ToLower(b)
	for _, n := range negWords {
		if strings.Contains(al, n) != strings.Contains(bl, n) {
			return true
		}
	}
	return false
}

// MemoryWorker processes MemoryMessage channel items.
func MemoryWorker(ctx context.Context, db *sql.DB, vi core.VectorIndex, extractor core.LLMExtractor, embedder core.Embedder, dedupThreshold float32, schema core.SchemaConfig, ch <-chan core.MemoryMessage) {
	worker := NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold, schema)
	for msg := range ch {
		prov := core.Provenance{ConversationID: msg.ConversationID, MessageID: msg.MessageID, ExtractedFrom: msg.Dialog}
		if err := worker.ProcessDialogWithProvenance(ctx, msg.Dialog, prov); err != nil {
			slog.Error("dialog processing failed", "err", err, "dialog_len", len(msg.Dialog))
		}
	}
}

// SimpleLLMExtractor is a stub for testing.
type SimpleLLMExtractor struct{}

func (e *SimpleLLMExtractor) ExtractEntities(ctx interface{}, dialog string) (*core.ExtractionResult, error) {
	result := &core.ExtractionResult{}
	for i, line := range strings.Split(dialog, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result.Entities = append(result.Entities, core.ExtractedEntity{
			ID: fmt.Sprintf("entity-%d", i+1), Category: "world", Content: line,
		})
	}
	return result, nil
}
