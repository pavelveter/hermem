package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/ingestion/detectors"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// ProcessDialog is the entry point for dialogs without provenance.
func (w *IngestionWorker) ProcessDialog(ctx context.Context, dialog string) error {
	return w.ProcessDialogWithProvenance(ctx, dialog, core.Provenance{ExtractedFrom: dialog})
}

// ProcessDialogWithProvenance loads, embeds, and stores entities from one dialog.
//
// Pipeline: Extract → Embed → SearchBatch → Normalize → ProcessEachItem
// (dedup → contradiction → merge/create → vi-ops)
func (w *IngestionWorker) ProcessDialogWithProvenance(ctx context.Context, dialog string, prov core.Provenance) error {
	const extractionTimeout = 5 * time.Minute
	ctx, cancel := context.WithTimeout(ctx, extractionTimeout)
	defer cancel()

	result, err := w.extractor.ExtractEntities(ctx, dialog)
	if err != nil {
		return fmt.Errorf("extract entities: %w", err)
	}
	items := make([]processInput, 0, len(result.Entities))
	for _, entity := range result.Entities {
		embedding, err := w.embedder.Embed(ctx, entity.Content)
		if err != nil {
			slog.Error("entity embed failed", "entity_id", entity.ID, "err", err)
			continue
		}
		items = append(items, processInput{entity: entity, embedding: embedding})
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

	for i := range items {
		vector.NormalizeVector(items[i].embedding)
	}

	for i, it := range items {
		if err := w.processOneItem(ctx, prov, items[i], allIDs[i], it.entity.ID); err != nil {
			slog.Error("item processing failed", "entity_id", it.entity.ID, "err", err)
		}
	}
	return nil
}

func (w *IngestionWorker) processOneItem(ctx context.Context, prov core.Provenance, it processInput, similarIDs []string, selfID string) error {
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
	return nil
}

type viOpKind int

const (
	viOpStore viOpKind = iota
	viOpRemove
)

type viOp struct {
	kind viOpKind
	id   string
	vec  []float32
}

func applyVIOps(ctx context.Context, vi core.VectorIndex, ops []viOp) {
	for _, op := range ops {
		switch op.kind {
		case viOpStore:
			if err := vi.Store(ctx, op.id, op.vec); err != nil {
				slog.Warn("post-commit vi.Store failed", "event", "vi_drift", "entity_id", op.id, "err", err)
			}
		case viOpRemove:
			if err := vi.Remove(ctx, []string{op.id}); err != nil {
				slog.Warn("post-commit vi.Remove failed", "event", "vi_drift", "entity_id", op.id, "err", err)
			}
		}
	}
}

func (w *IngestionWorker) processOneItemOnce(ctx context.Context, prov core.Provenance, it processInput, similarIDs []string, selfID string) error {
	targetID := it.entity.ID
	existing, err := w.findMatch(it.embedding, similarIDs, selfID)
	if err != nil {
		return fmt.Errorf("entity match failed: %w", err)
	}

	var viOps []viOp
	var archiveID string
	if existing != nil {
		action, archID, ops := w.handleContradiction(existing, it.entity)
		archiveID = archID
		viOps = append(viOps, ops...)
		switch action {
		case contradictionKeepBoth:
			it.entity.Relations = append(it.entity.Relations, core.Relation{TargetID: existing.ID, RelationType: w.schema.RelationContradicts})
			existing = nil
		case contradictionPreferIncoming:
			viOps = append(viOps, viOp{kind: viOpStore, id: it.entity.ID, vec: it.embedding})
			existing = nil
		}
	}

	var merged *core.Entity
	if existing != nil {
		merged, err = w.mergeExistingEntity(ctx, existing, it.entity, prov)
		if err != nil {
			return fmt.Errorf("merge embed failed: %w", err)
		}
		targetID = existing.ID
		viOps = append(viOps,
			viOp{kind: viOpRemove, id: it.entity.ID},
			viOp{kind: viOpStore, id: merged.ID, vec: merged.Embedding},
		)
	} else if archiveID == "" {
		viOps = append(viOps, viOp{kind: viOpStore, id: it.entity.ID, vec: it.embedding})
	}

	if err := w.executeItemTx(ctx, targetID, it.entity, it.embedding, prov, merged, archiveID); err != nil {
		return err
	}
	applyVIOps(ctx, w.vi, viOps)
	return nil
}

type processInput struct {
	entity    core.ExtractedEntity
	embedding []float32
}

func (w *IngestionWorker) executeItemTx(ctx context.Context, targetID string, entity core.ExtractedEntity, embedding []float32, prov core.Provenance, merged *core.Entity, archiveID string) error {
	itemTx, err := w.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin tx failed: %w", err)
	}
	var writeErr error
	if merged != nil {
		writeErr = w.mergeEntityInTx(ctx, itemTx, *merged)
	} else {
		writeErr = w.createEntityInTx(ctx, itemTx, entity, embedding, prov)
	}
	if archiveID != "" && writeErr == nil {
		if _, err := itemTx.ExecContext(ctx, `UPDATE entities SET archived = 1 WHERE id = ?`, archiveID); err != nil {
			writeErr = fmt.Errorf("archive existing in tx: %w", err)
		}
	}
	if writeErr == nil {
		writeErr = w.createEdgesInTx(ctx, itemTx, targetID, entity.Relations)
	}
	if writeErr != nil {
		_ = itemTx.Rollback()
		return writeErr
	}
	return itemTx.Commit()
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

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sqlite3.ErrBusy) || errors.Is(err, sqlite3.ErrLocked) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "SQLITE_BUSY")
}

// IsIngestionContradiction guards dedup by negation heuristic.
func IsIngestionContradiction(a, b string) bool {
	return detectors.NewLexicalDetector().Detect(
		core.Entity{Content: a},
		core.Entity{Content: b},
	).Detected
}

// MemoryWorker processes MemoryMessage channel items without durability.
func MemoryWorker(ctx context.Context, db *sql.DB, vi core.VectorIndex, extractor core.LLMExtractor, embedder core.Embedder, dedupThreshold float32, schema core.SchemaConfig, ch <-chan core.MemoryMessage) {
	worker := NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold, schema, detectors.NewLexicalDetector())
	const maxParallel = 1
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for msg := range ch {
		msg := msg
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

// resilientConfig holds the shared parameters for the resilient event loop.
type resilientConfig struct {
	worker      *IngestionWorker
	ckptPath    string
	pendingPath string
	workerID    string
	maxParallel int
}

// resilientLoop is the shared dispatch→drain→checkpoint loop used by both
// MemoryWorkerResilient and MemoryWorkerResilientFromConfig.
func resilientLoop(ctx context.Context, cfg resilientConfig, ch <-chan core.MemoryMessage) {
	if cfg.ckptPath == "" && cfg.pendingPath == "" {
		slog.Warn("MemoryWorkerResilient: ckptPath and pendingPath both empty — no durability on cancel",
			"worker_id", cfg.workerID)
	}
	LoadCheckpoint(cfg.ckptPath, cfg.workerID)
	sem := make(chan struct{}, cfg.maxParallel)
	var wg sync.WaitGroup
	var processed atomic.Int64

	flushCheckpoint := func() {
		cur := IngestionCheckpoint{
			LastCommittedIndex: processed.Load(),
			LastCommittedAt:    time.Now().UTC(),
			WorkerID:           cfg.workerID,
		}
		if err := SaveCheckpoint(cfg.ckptPath, cur); err != nil {
			slog.Error("checkpoint save failed", "err", err, "path", cfg.ckptPath)
		}
	}

	drain := func() {
		pending := make([]core.MemoryMessage, 0, 16)
		deadline := time.NewTimer(defaultDrainTimeout)
		defer deadline.Stop()
	drainLoop:
		for {
			select {
			case remaining, ok := <-ch:
				if !ok {
					break drainLoop
				}
				pending = append(pending, remaining)
			case <-deadline.C:
				slog.Warn("MemoryWorkerResilient: drain deadline reached, producer did not close ch",
					"worker_id", cfg.workerID, "pending_count", len(pending))
				break drainLoop
			}
		}
		if err := SavePendingQueue(cfg.pendingPath, pending); err != nil {
			slog.Error("pending save failed", "err", err, "path", cfg.pendingPath)
		} else if len(pending) > 0 {
			slog.Info("MemoryWorkerResilient: drained to pending queue",
				"count", len(pending), "path", cfg.pendingPath)
		}
	}

	for {
		select {
		case <-ctx.Done():
			slog.Debug("MemoryWorkerResilient: ctx cancelled, draining", "worker_id", cfg.workerID)
			drain()
			wg.Wait()
			flushCheckpoint()
			return
		case msg, ok := <-ch:
			if !ok {
				wg.Wait()
				flushCheckpoint()
				return
			}
			wg.Add(1)
			select {
			case <-ctx.Done():
				wg.Done()
				drain()
				wg.Wait()
				flushCheckpoint()
				return
			case sem <- struct{}{}:
			}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				prov := core.Provenance{ConversationID: msg.ConversationID, MessageID: msg.MessageID, ExtractedFrom: msg.Dialog}
				if err := cfg.worker.ProcessDialogWithProvenance(ctx, msg.Dialog, prov); err != nil {
					slog.Error("dialog processing failed",
						"err", err, "dialog_len", len(msg.Dialog), "worker_id", cfg.workerID)
					return
				}
				processed.Add(1)
				cur := IngestionCheckpoint{
					LastCommittedIndex: processed.Load(),
					LastCommittedAt:    time.Now().UTC(),
					WorkerID:           cfg.workerID,
				}
				if err := SaveCheckpoint(cfg.ckptPath, cur); err != nil {
					slog.Warn("per-msg checkpoint save failed",
						"err", err, "index", cur.LastCommittedIndex)
				}
			}()
		}
	}
}

// MemoryWorkerResilientFromConfig is the production-grade ingest entry point.
func MemoryWorkerResilientFromConfig(ctx context.Context, cfg MemoryWorkerConfig, ch <-chan core.MemoryMessage) {
	worker := NewIngestionWorkerFromConfig(IngestionWorkerConfig{
		DB:             cfg.DB,
		VectorIndex:    cfg.VectorIndex,
		Extractor:      cfg.Extractor,
		Embedder:       cfg.Embedder,
		DedupThreshold: cfg.DedupThreshold,
		Schema:         cfg.Schema,
	})
	resilientLoop(ctx, resilientConfig{
		worker:      worker,
		ckptPath:    cfg.CkptPath,
		pendingPath: cfg.PendingPath,
		workerID:    cfg.WorkerID,
		maxParallel: 8,
	}, ch)
}

// MemoryWorkerResilient is the production-grade ingest entry point.
// Deprecated: Use MemoryWorkerResilientFromConfig instead.
func MemoryWorkerResilient(ctx context.Context, db *sql.DB, vi core.VectorIndex, extractor core.LLMExtractor, embedder core.Embedder, dedupThreshold float32, schema core.SchemaConfig, ckptPath, pendingPath, workerID string, ch <-chan core.MemoryMessage) {
	worker := NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold, schema, detectors.NewLexicalDetector())
	resilientLoop(ctx, resilientConfig{
		worker:      worker,
		ckptPath:    ckptPath,
		pendingPath: pendingPath,
		workerID:    workerID,
		maxParallel: 8,
	}, ch)
}
