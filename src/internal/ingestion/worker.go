// Package ingestion provides the dialog-to-entity ingestion pipeline.
package ingestion

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// IngestionWorker handles the extraction→embed→store pipeline.
type IngestionWorker struct {
	db          *sql.DB
	vi          core.VectorIndex
	extractor   core.LLMExtractor
	embedder    core.Embedder
	dedupThresh float32
	schema      core.SchemaConfig
	detector    contradiction.ContradictionDetector
}

// NewIngestionWorker creates a worker.
//
// Deprecated: Use NewIngestionWorkerFromConfig instead.
func NewIngestionWorker(db *sql.DB, vi core.VectorIndex, extractor core.LLMExtractor, embedder core.Embedder, dedupThreshold float32, schema core.SchemaConfig, detector contradiction.ContradictionDetector) *IngestionWorker {
	return NewIngestionWorkerFromConfig(IngestionWorkerConfig{
		DB:             db,
		VectorIndex:    vi,
		Extractor:      extractor,
		Embedder:       embedder,
		DedupThreshold: dedupThreshold,
		Schema:         schema,
		Detector:       detector,
	})
}

// ReloadSchema swaps the schema on SIGHUP.
func (w *IngestionWorker) ReloadSchema(schema core.SchemaConfig) { w.schema = schema }

// createEntityInTx inserts a freshly extracted entity with embedding and provenance.
//
// Precondition: `embedding` is already unit-length-normalized by the
// caller (ProcessDialogWithProvenance runs vector.NormalizeVector for
// every item before per-item tx dispatch). The DB BLOB and the
// post-commit vi.Store write the SAME vec slice, so both sides must
// observe the normalized form — otherwise the DB-stored vec and the
// vec index drift apart on cosine similarity at the next SearchBatch.
func (w *IngestionWorker) createEntityInTx(ctx context.Context, tx *sql.Tx, entity core.ExtractedEntity, embedding []float32, prov core.Provenance) error {
	dbEntity := core.Entity{
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

// mergeEntityInTx updates an existing entity with merged content + new embedding.
//
// Precondition: `e.Embedding` is already unit-length-normalized by the
// caller (processOneItemOnce runs vector.NormalizeVector after the
// merge-prep Embed call). Same rationale as createEntityInTx — keep
// DB BLOB and post-commit vi.Store on the same vec form.
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

// createEdgesInTx bulk-inserts edges in chunks of 166 (SQLite variable limit) filtered by schema.
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
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO edges (source_id, target_id, relation_type, weight) VALUES `+strings.Join(phs, ","), args...); err != nil {
			return fmt.Errorf("bulk insert edges: %w", err)
		}
	}
	return nil
}
