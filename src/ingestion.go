package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type IngestionWorker struct {
	db          *sql.DB
	vi          VectorIndex
	extractor   LLMExtractor
	embedder    Embedder
	dedupThresh float32
	schema      SchemaConfig
}

// highConfidenceThreshold is the floor for treating an entity's confidence
// as "reliable" during contradiction resolution. When an existing entity
// with confidence below this threshold contradicts a freshly extracted
// entity (confidence 1.0), the existing entity is archived in favor of
// the new one rather than keeping both with a contradicts edge.
const highConfidenceThreshold float32 = 0.7

func NewIngestionWorker(db *sql.DB, vi VectorIndex, extractor LLMExtractor, embedder Embedder, dedupThreshold float32, schema SchemaConfig) *IngestionWorker {
	return &IngestionWorker{
		db:          db,
		vi:          vi,
		extractor:   extractor,
		embedder:    embedder,
		dedupThresh: dedupThreshold,
		schema:      schema,
	}
}

// ReloadSchema swaps the worker's schema. Called on SIGHUP after the
// server reloads its own state. The maps inside SchemaConfig are never
// mutated after construction, so the struct-value replacement is safe
// without synchronization — readers always see a fully-constructed,
// immutable map tree regardless of which assignment they observe.
func (w *IngestionWorker) ReloadSchema(schema SchemaConfig) {
	w.schema = schema
}

// Provenance records where an ingested entity came from.
type Provenance struct {
	ConversationID string
	MessageID      string
	ExtractedFrom  string // the dialog that produced this entity
}

// ProcessDialog is the backward-compatible entry point. It delegates to
// ProcessDialogWithProvenance with default provenance sourced from the
// dialog itself.
func (w *IngestionWorker) ProcessDialog(ctx context.Context, dialog string) error {
	return w.ProcessDialogWithProvenance(ctx, dialog, Provenance{ExtractedFrom: dialog})
}

// ProcessDialogWithProvenance loads, embeds, stores entities from one dialog,
// recording provenance metadata on each stored entity.
//
// Sprint 1 transaction model (per-item, entity+edges atomic):
//
//	for each item in result.Entities:
//	  vi.Store(entityID, embedding)       // outside SQL txn — network-shaped
//	  tx.Begin
//	    INSERT OR REPLACE entity          // status default from schema
//	    INSERT edges (chunked)            // FK enforcement gate
//	  tx.Commit                           // entity+edges land atomically
//	  on any failure: tx.Rollback + vi.Remove
//
// The entity INSERT and edges INSERT live inside the same SQL
// transaction so that an edge failure rolls back both writes — no
// half-written graph states (Sprint 1 goal).
//
// Sprint 2: provenance tracking. Each stored entity records
// conversation_id, message_id, extracted_from, source = "dialog",
// source_type = "extraction", confidence = 1.0.
func (w *IngestionWorker) ProcessDialogWithProvenance(ctx context.Context, dialog string, prov Provenance) error {
	ctx, span := Tracer().Start(ctx, "ingestion.process_dialog")
	defer span.End()

	result, err := w.extractor.ExtractEntities(ctx, dialog)
	if err != nil {
		span.RecordError(err)
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

	// Phase 5: BulkStore all embeddings in a single lock acquisition
	// before the per-item loop. Per-item failures still roll back via
	// vi.Remove for individual IDs (same pattern as before).
	var bulkPairs []BulkPair
	for _, it := range items {
		NormalizeVector(it.embedding)
		bulkPairs = append(bulkPairs, BulkPair{ID: it.entity.ID, Vec: it.embedding})
	}
	if err := w.vi.BulkStore(ctx, bulkPairs); err != nil {
		return fmt.Errorf("bulk vector index store: %w", err)
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

		// Sprint 5: contradiction detection — when a near-duplicate is
		// found by cosine similarity but the new statement contradicts
		// the existing one (e.g. "likes Go" vs "hates Go"), keep both
		// as separate nodes with a contradicts edge instead of merging.
		//
		// Phase 3: confidence comparison — when the existing entity
		// has low confidence (< highConfidenceThreshold), archive it
		// in favor of the freshly extracted incoming (confidence 1.0).
		// When both are high-confidence, keep both with a contradicts edge.
		if existing != nil && isContradiction(existing.Content, it.entity.Content) {
			existingConf := existing.Confidence
			if existingConf == 0 {
				// Unset confidence (NULL→0, pre-migration entities):
				// treat as reliable to avoid archiving all old entities
				// on their first contradiction.
				existingConf = 1.0
			}

			if existingConf >= highConfidenceThreshold {
				// Both sides confident — keep both with contradicts edge.
				slog.Info("contradiction detected, keeping both nodes",
					"event", "contradiction",
					"existing_id", existing.ID,
					"incoming_id", it.entity.ID,
					"existing_conf", existingConf,
					"existing", truncate(existing.Content, 60),
					"incoming", truncate(it.entity.Content, 60),
				)
				it.entity.Relations = append(it.entity.Relations, Relation{
					TargetID:     existing.ID,
					RelationType: "contradicts",
				})
				existing = nil // force create-new path, not merge
			} else {
				// Existing has low confidence — prefer incoming.
				// Archive the existing entity and its vector index entry.
				slog.Info("contradiction resolved: preferring incoming (higher confidence)",
					"event", "contradiction_resolved",
					"existing_id", existing.ID,
					"incoming_id", it.entity.ID,
					"existing_conf", existingConf,
					"existing", truncate(existing.Content, 60),
					"incoming", truncate(it.entity.Content, 60),
				)
				if _, err := w.db.ExecContext(ctx,
					`UPDATE entities SET archived = 1 WHERE id = ?`,
					existing.ID,
				); err != nil {
					slog.Warn("failed to archive low-confidence entity",
						"entity_id", existing.ID, "err", err,
					)
				}
				w.vi.Remove(ctx, []string{existing.ID})
				existing = nil // create new node (no contradicts edge needed)
			}
		}

		// Vector already stored in the index via BulkStore above.
		// Per-item failure rollback uses vi.Remove; BulkStore is
		// single-lock, Remove is fine-grained.

		// ---- pre-tx phase: re-embed if merging, update vector index -----
		var mergeEntity *Entity
		if existing != nil {
			targetID = existing.ID
			mergedContent := existing.Content
			if !strings.Contains(existing.Content, it.entity.Content) {
				mergedContent = existing.Content + "; " + it.entity.Content
			}
			updatedEmb, embErr := w.embedder.Embed(ctx, mergedContent)
			if embErr != nil {
				w.vi.Remove(ctx, []string{it.entity.ID})
				slog.Error("entity processing failed, continuing",
					"event", "entity_failed",
					"entity_id", it.entity.ID,
					"err", embErr,
				)
				continue
			}
			NormalizeVector(updatedEmb)
			now := time.Now()
			mergeEntity = &Entity{
				ID:        existing.ID,
				Category:  existing.Category,
				Content:   mergedContent,
				Embedding: updatedEmb,
				Status:    existing.Status,
				// Preserve original creation metadata on merge
				CreatedAt:  existing.CreatedAt,
				Confidence: 1.0,
				// Update provenance to latest source
				ConversationID: prov.ConversationID,
				MessageID:      prov.MessageID,
				ExtractedFrom:  prov.ExtractedFrom,
				Source:         "dialog",
				SourceType:     "extraction",
				UpdatedAt:      now,
			}
			// Remove the incoming entity's dangling index entry.
			w.vi.Remove(ctx, []string{it.entity.ID})
		}

		// ---- tx phase: entity + edges land atomically --------------------
		itemTx, itemErr := w.db.BeginTx(ctx, nil)
		if itemErr != nil {
			if mergeEntity != nil {
				// Pre-tx already removed it.entity.ID; mergeEntity.ID
				// was never touched — vi.Store deferred to post-commit.
			} else {
				w.vi.Remove(ctx, []string{it.entity.ID})
			}
			slog.Error("entity processing failed, continuing",
				"event", "entity_failed",
				"entity_id", it.entity.ID,
				"err", itemErr,
			)
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
				if rmErr := w.vi.Remove(ctx, []string{rollbackID}); rmErr != nil {
					slog.Warn("vector index rollback after item failure",
						"event", "vector_rollback_fail",
						"entity_id", rollbackID,
						"rm_err", rmErr,
					)
				}
			}
			slog.Error("entity processing failed, continuing",
				"event", "entity_failed",
				"entity_id", it.entity.ID,
				"err", writeErr,
			)
			continue
		}

		if err := itemTx.Commit(); err != nil {
			if rollbackID != "" {
				if rmErr := w.vi.Remove(ctx, []string{rollbackID}); rmErr != nil {
					slog.Warn("vector index rollback after commit failure",
						"event", "vector_rollback_fail",
						"entity_id", rollbackID,
						"rm_err", rmErr,
					)
				}
			}
			slog.Error("entity processing failed, continuing",
				"event", "entity_failed",
				"entity_id", it.entity.ID,
				"err", err,
			)
			continue
		}

		// Post-commit: update the existing entity's vi entry.
		if mergeEntity != nil {
			w.vi.Store(ctx, mergeEntity.ID, mergeEntity.Embedding)
		}
	}
	return nil
}

// mergeEntityInTx INSERTs an already-merged entity on the supplied
// transaction. The caller must have already re-embedded the merged
// content (network call) before opening the transaction — this method
// is SQL-only so the tx is never held open across outbound I/O.
func (w *IngestionWorker) mergeEntityInTx(ctx context.Context, tx *sql.Tx, e Entity) error {
	embeddingBytes := EmbeddingToBytes(e.Embedding)
	status := e.Status
	if status == "" && w.schema.StatefulCategories[e.Category] && len(w.schema.ValidStateOrder) > 0 {
		status = w.schema.ValidStateOrder[0]
	}
	_, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO entities
			(id, category, content, embedding, updated_at, status,
			 confidence, source, source_type, created_at,
			 conversation_id, message_id, extracted_from)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?,
		        ?, ?, ?, ?,
		        ?, ?, ?)
	`,
		e.ID, e.Category, e.Content, embeddingBytes, nullString(status),
		e.Confidence, e.Source, e.SourceType, orNullTime(e.CreatedAt),
		e.ConversationID, e.MessageID, e.ExtractedFrom,
	)
	if err != nil {
		return fmt.Errorf("merge entity: %w", err)
	}
	return nil
}

// createEntityInTx normalises and INSERTs one entity on the supplied
// transaction with provenance metadata. Does NOT open or commit its
// own transaction.
func (w *IngestionWorker) createEntityInTx(ctx context.Context, tx *sql.Tx, entity ExtractedEntity, embedding []float32, prov Provenance) error {
	NormalizeVector(embedding)
	dbEntity := Entity{
		ID:             entity.ID,
		Category:       entity.Category,
		Content:        entity.Content,
		Embedding:      embedding,
		Confidence:     1.0,
		Source:         "dialog",
		SourceType:     "extraction",
		ConversationID: prov.ConversationID,
		MessageID:      prov.MessageID,
		ExtractedFrom:  prov.ExtractedFrom,
	}
	embeddingBytes := EmbeddingToBytes(dbEntity.Embedding)
	status := dbEntity.Status
	if status == "" && w.schema.StatefulCategories[dbEntity.Category] && len(w.schema.ValidStateOrder) > 0 {
		status = w.schema.ValidStateOrder[0]
	}

	_, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO entities
			(id, category, content, embedding, updated_at, status,
			 confidence, source, source_type, created_at,
			 conversation_id, message_id, extracted_from)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?,
		        ?, ?, ?, CURRENT_TIMESTAMP,
		        ?, ?, ?)
	`,
		dbEntity.ID, dbEntity.Category, dbEntity.Content, embeddingBytes, nullString(status),
		dbEntity.Confidence, dbEntity.Source, dbEntity.SourceType,
		dbEntity.ConversationID, dbEntity.MessageID, dbEntity.ExtractedFrom,
	)
	if err != nil {
		return fmt.Errorf("insert entity: %w", err)
	}
	return nil
}

// createEdgesInTx inserts all relations on the supplied transaction.
// Does NOT open or commit its own transaction.
func (w *IngestionWorker) createEdgesInTx(ctx context.Context, tx *sql.Tx, entityID string, relations []Relation) error {
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
			if !w.schema.AllowedRelations[rel.RelationType] {
				return fmt.Errorf("unknown relation_type: %s", rel.RelationType)
			}
			args = append(args, entityID, rel.TargetID, rel.RelationType, 1.0)
			phs[i] = "(?, ?, ?, ?)"
		}
		q := `INSERT OR IGNORE INTO edges (source_id, target_id, relation_type, weight) VALUES ` +
			strings.Join(phs, ",")
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("bulk insert edges for %s: chunk [%d-%d] of %d: %w",
				entityID, start, end, len(relations), err)
		}
	}
	return nil
}

// createEdgesItem is the test-facing wrapper that opens+commits its own
// transaction. ProcessDialog uses createEdgesInTx directly on its
// per-item tx. Kept public so TestCreateEdgesBulk* can test the
// chunk-insert logic without constructing a ProcessDialog harness.
func (w *IngestionWorker) createEdgesItem(ctx context.Context, entityID string, relations []Relation) error {
	if len(relations) == 0 {
		return nil
	}
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := w.createEdgesInTx(ctx, tx, entityID, relations); err != nil {
		return err
	}
	return tx.Commit()
}

func (w *IngestionWorker) findMatch(embedding []float32, similarIDs []string, selfID string) (*Entity, error) {
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

	var entity Entity
	var embeddingBytes []byte
	var confidence sql.NullFloat64
	var source, sourceType, conversationID, messageID, extractedFrom sql.NullString
	var createdAt sql.NullTime
	err := w.db.QueryRow(
		`SELECT id, category, content, embedding, updated_at,
		        confidence, source, source_type, created_at,
		        conversation_id, message_id, extracted_from
		 FROM entities WHERE id = ?`,
		candidateID,
	).Scan(
		&entity.ID, &entity.Category, &entity.Content, &embeddingBytes, &entity.UpdatedAt,
		&confidence, &source, &sourceType, &createdAt,
		&conversationID, &messageID, &extractedFrom,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to fetch candidate %q: %w", candidateID, err)
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
	if conversationID.Valid {
		entity.ConversationID = conversationID.String
	}
	if messageID.Valid {
		entity.MessageID = messageID.String
	}
	if extractedFrom.Valid {
		entity.ExtractedFrom = extractedFrom.String
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

type MemoryMessage struct {
	Dialog         string
	ConversationID string
	MessageID      string
}

func MemoryWorker(ctx context.Context, db *sql.DB, vi VectorIndex, extractor LLMExtractor, embedder Embedder, dedupThreshold float32, schema SchemaConfig, ch <-chan MemoryMessage) {
	worker := NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold, schema)
	for msg := range ch {
		prov := Provenance{
			ConversationID: msg.ConversationID,
			MessageID:      msg.MessageID,
			ExtractedFrom:  msg.Dialog,
		}
		if err := worker.ProcessDialogWithProvenance(ctx, msg.Dialog, prov); err != nil {
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
