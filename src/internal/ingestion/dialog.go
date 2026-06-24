package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

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

	bulkPairs := make([]core.BulkPair, 0, len(items))
	for _, it := range items {
		vector.NormalizeVector(it.embedding)
		bulkPairs = append(bulkPairs, core.BulkPair{ID: it.entity.ID, Vec: it.embedding})
	}
	if err := w.vi.BulkStore(ctx, bulkPairs); err != nil {
		return fmt.Errorf("bulk store: %w", err)
	}

	for i, it := range items {
		if err := w.processOneItem(ctx, prov, items[i], allIDs[i], bulkPairs[i].ID); err != nil {
			slog.Error("item processing failed", "entity_id", it.entity.ID, "err", err)
		}
		_ = i
	}
	return nil
}

// processOneItem handles dedup, contradiction resolution and atomic write for a single extracted entity.
//
// Wraps processOneItemOnce with a retry loop that absorbs transient
// SQLITE_BUSY ("database is locked") errors from db.BeginTx / tx.Commit
// under writer-side contention from the GC + parallel ingestion.
//
// Non-busy errors are returned immediately — retrying them would amplify
// the bug rather than fix it.
func (w *IngestionWorker) processOneItem(ctx context.Context, prov core.Provenance, it struct {
	entity    core.ExtractedEntity
	embedding []float32
}, similarIDs []string, selfID string) error {
	const maxAttempts = 5
	backoff := 50 * time.Millisecond
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := w.processOneItemOnce(ctx, prov, it, similarIDs, selfID)
		if err == nil {
			return nil
		}
		if !isSQLiteBusyError(err) {
			return err
		}
		if attempt == maxAttempts {
			return fmt.Errorf("processOneItem: exhausted retries on busy: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return nil // unreachable; loop returns above
}

// processOneItemOnce is the unwrapped tx body. Do not call directly;
// always call processOneItem which retries on busy.
func (w *IngestionWorker) processOneItemOnce(ctx context.Context, prov core.Provenance, it struct {
	entity    core.ExtractedEntity
	embedding []float32
}, similarIDs []string, selfID string) error {
	targetID := it.entity.ID
	existing, err := w.findMatch(it.embedding, similarIDs, selfID)
	if err != nil {
		return fmt.Errorf("entity match failed: %w", err)
	}

	if existing != nil && IsIngestionContradiction(existing.Content, it.entity.Content) {
		existingConf := existing.Confidence
		if existingConf == 0 {
			existingConf = 1.0
		}
		if existingConf >= 0.7 {
			slog.Info("contradiction detected, keeping both", "existing_id", existing.ID, "incoming_id", it.entity.ID)
			it.entity.Relations = append(it.entity.Relations, core.Relation{TargetID: existing.ID, RelationType: w.schema.RelationContradicts})
			existing = nil
		} else {
			slog.Info("contradiction resolved: preferring incoming", "existing_id", existing.ID, "incoming_id", it.entity.ID)
			if _, err := w.db.ExecContext(ctx, `UPDATE entities SET archived = 1 WHERE id = ?`, existing.ID); err != nil {
				slog.Warn("contradiction archive failed", "id", existing.ID, "err", err)
			}
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
			return fmt.Errorf("merge embed failed: %w", embErr)
		}
		vector.NormalizeVector(updatedEmb)
		mergeEntity = &core.Entity{
			ID:             existing.ID,
			Category:       existing.Category,
			Content:        mergedContent,
			Embedding:      updatedEmb,
			Status:         existing.Status,
			CreatedAt:      existing.CreatedAt,
			Confidence:     1.0,
			ConversationID: prov.ConversationID,
			MessageID:      prov.MessageID,
			ExtractedFrom:  prov.ExtractedFrom,
			Source:         "dialog",
			SourceType:     "extraction",
			UpdatedAt:      time.Now().UTC(),
		}
		w.vi.Remove(ctx, []string{it.entity.ID})
	}

	itemTx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		if mergeEntity == nil {
			w.vi.Remove(ctx, []string{it.entity.ID})
		}
		return fmt.Errorf("begin tx failed: %w", err)
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
		return writeErr
	}
	if err := itemTx.Commit(); err != nil {
		if rollbackID != "" {
			w.vi.Remove(ctx, []string{rollbackID})
		}
		return err
	}
	if mergeEntity != nil {
		w.vi.Store(ctx, mergeEntity.ID, mergeEntity.Embedding)
	}
	return nil
}

// findMatch returns the highest-similarity non-self entity above the dedup threshold.
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

// isSQLiteBusyError reports whether err is the transient SQLite writer-
// contention signal (SQLITE_BUSY / "database is locked"). Only these
// errors are retried in processOneItem; everything else (logic errors,
// constraint violations, schema mismatches) is returned immediately so
// retries don't mask real bugs.
func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sqlite3.ErrBusy) || errors.Is(err, sqlite3.ErrLocked) {
		return true
	}
	msg := err.Error()
	// Substring matches are kept narrow on purpose: a broader "busy"
	// match could collide with user-data embedded in error messages
	// (e.g. facts or entity names) and trigger spurious retries.
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "SQLITE_BUSY")
}

// IsIngestionContradiction guards dedup by negation heuristic: if an almost-identical
// existing entity flips on any one of these common-enough negation tokens that the
// incoming one doesn't (or vice versa), treat it as a contradiction rather than a
// merge. Cheap, language-light, no LLM round-trip — good enough to flag for the
// contradiction-resolution path in processOneItem.
func IsIngestionContradiction(a, b string) bool {
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

// MemoryWorker processes MemoryMessage channel items — batch entry point
// for parallel ingestion.
//
// Concurrency is bounded by a semaphore so a flooding producer cannot
// drive the worker into OOM or starve the SQLite single-writer (set in
// store.InitDB SetMaxOpenConns(1)) under sustained produce pressure.
//
// On ctx.Done() the loop returns cleanly without leaving dangling goroutines:
// in-flight goroutines observe ctx through ProcessDialogWithProvenance and
// unwind themselves; the WaitGroup barriers the function exit.
func MemoryWorker(ctx context.Context, db *sql.DB, vi core.VectorIndex, extractor core.LLMExtractor, embedder core.Embedder, dedupThreshold float32, schema core.SchemaConfig, ch <-chan core.MemoryMessage) {
	worker := NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold, schema)
	const maxParallel = 8
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for msg := range ch {
		msg := msg // capture loop variable for goroutine
		wg.Add(1)
		select {
		case <-ctx.Done():
			wg.Done()
			return
		case sem <- struct{}{}:
		}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			prov := core.Provenance{ConversationID: msg.ConversationID, MessageID: msg.MessageID, ExtractedFrom: msg.Dialog}
			if err := worker.ProcessDialogWithProvenance(ctx, msg.Dialog, prov); err != nil {
				slog.Error("dialog processing failed", "err", err, "dialog_len", len(msg.Dialog))
			}
		}()
	}
	wg.Wait()
}
