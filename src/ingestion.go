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
	schema      SchemaConfig
}

func NewIngestionWorker(db *sql.DB, vi VectorIndex, extractor LLMExtractor, embedder Embedder, dedupThreshold float32, schema SchemaConfig) *IngestionWorker {
	return &IngestionWorker{
		db:          db,
		vi:          vi,
		extractor:   extractor,
		embedder:    embedder,
		dedupThresh: dedupThreshold,
		schema:      schema,
	}
} // ProcessDialog loads, embeds, stores entities from one dialog.
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
// half-written graph states (Sprint 1 goal). The per-item boundary
// is chosen because:
//   - LLM extraction is already the per-item bottleneck; the
//     per-item SQL round-trip amortizes into that cost.
//   - A failure on item N does not abort items N+1: each item has
//     independent atomicity.
//   - vi.Store precedes the tx so the index never lags (invariant
//     preserved from pre-Sprint-1 code).
//
// What stays outside the SQL txn:
//   - embedder.Embed: network call; must NOT hold a write transaction
//     open that would block concurrent reads.
//   - vi.Store: maintains the "index never points to row the DB
//     doesn't hold" invariant.
//
// Failure mode summary:
//   - LLM extract failure → no DB writes; log + return.
//   - Per-item embed failure → slog + continue to next item.
//   - Any SQL failure (entity, edges, commit) → tx.Rollback + vi.Remove.
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

		// Normalize before storing in vi: the index assumes unit
		// vectors (Search divides by queryNorm only, not entry norms).
		// NormalizeVector is non-blocking and safe to call pre-tx.
		NormalizeVector(it.embedding)

		// store/update the index BEFORE the SQL txn runs. This keeps
		// the "no lag" invariant for concurrent Search while we wait
		// for the COMMIT: if a parallel Search hits the index entry,
		// it sees the row in the DB (the eventual commit happens
		// before index entry removal becomes necessary, indexes only
		// roll back on failure).
		if err := w.vi.Store(ctx, it.entity.ID, it.embedding); err != nil {
			slog.Error("entity processing failed, continuing",
				"event", "entity_failed",
				"entity_id", it.entity.ID,
				"err", err,
			)
			continue
		}

		// ---- pre-tx phase: re-embed if merging, update vector index -----
		// Network calls and vi mutations happen here, BEFORE any SQL
		// transaction opens, so write locks are never held across I/O.
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
			mergeEntity = &Entity{
				ID:        existing.ID,
				Category:  existing.Category,
				Content:   mergedContent,
				Embedding: updatedEmb,
				Status:    existing.Status,
			}
			// Remove the incoming entity's dangling index entry — after
			// merge, the only entity in the DB is the existing one.
			// The existing entity's vi entry is NOT overwritten here:
			// vi.Store(existing.ID, updatedEmb) is deferred to after
			// tx.Commit so a tx failure doesn't lose the old embedding.
			w.vi.Remove(ctx, []string{it.entity.ID})
		}

		// ---- tx phase: entity + edges land atomically --------------------
		itemTx, itemErr := w.db.BeginTx(ctx, nil)
		if itemErr != nil {
			if mergeEntity != nil {
				// Pre-tx already removed it.entity.ID; mergeEntity.ID
				// (existing) was never touched in this iteration — its
				// vi.Store is deferred to post-commit. No cleanup needed.
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
			writeErr = w.createEntityInTx(ctx, itemTx, it.entity, it.embedding)
		}
		if writeErr == nil {
			writeErr = w.createEdgesInTx(ctx, itemTx, targetID, it.entity.Relations)
		}

		rollbackID := it.entity.ID
		if mergeEntity != nil {
			// Pre-tx already removed it.entity.ID from vi.
			// mergeEntity.ID (existing) vi entry was never updated
			// — vi.Store is deferred to post-commit. Skip vi.Remove
			// on failure to preserve the existing entity's valid vec.
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

		// Post-commit: update the existing entity's vi entry with
		// the merged embedding. Deferred to here so a tx failure
		// leaves the old (functional) embedding intact.
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
		INSERT OR REPLACE INTO entities (id, category, content, embedding, updated_at, status)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?)
	`, e.ID, e.Category, e.Content, embeddingBytes, nullString(status))
	if err != nil {
		return fmt.Errorf("merge entity: %w", err)
	}
	return nil
}

// createEntityInTx normalises and INSERTs one entity on the supplied
// transaction. Does NOT open or commit its own transaction.
func (w *IngestionWorker) createEntityInTx(ctx context.Context, tx *sql.Tx, entity ExtractedEntity, embedding []float32) error {
	NormalizeVector(embedding)
	dbEntity := Entity{
		ID:        entity.ID,
		Category:  entity.Category,
		Content:   entity.Content,
		Embedding: embedding,
	}
	embeddingBytes := EmbeddingToBytes(dbEntity.Embedding)
	status := dbEntity.Status
	if status == "" && w.schema.StatefulCategories[dbEntity.Category] && len(w.schema.ValidStateOrder) > 0 {
		status = w.schema.ValidStateOrder[0]
	}

	_, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO entities (id, category, content, embedding, updated_at, status)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?)
	`, dbEntity.ID, dbEntity.Category, dbEntity.Content, embeddingBytes, nullString(status))
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
			args = append(args, entityID, rel.TargetID, rel.RelationType)
			phs[i] = "(?, ?, ?)"
		}
		q := `INSERT OR IGNORE INTO edges (source_id, target_id, relation_type) VALUES ` +
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

type MemoryMessage struct {
	Dialog string
}

func MemoryWorker(ctx context.Context, db *sql.DB, vi VectorIndex, extractor LLMExtractor, embedder Embedder, dedupThreshold float32, schema SchemaConfig, ch <-chan MemoryMessage) {
	worker := NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold, schema)
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
