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
// § 3.1 atomicity: every per-item vi (vector-index) operation runs ONLY
// AFTER w.db.BeginTx → itemTx.Commit() succeeds. The replacement path
// normalizes each embedding here (idempotent, fast) and lets
// processOneItemOnce write the vec entry after its own tx commits.
func (w *IngestionWorker) ProcessDialogWithProvenance(ctx context.Context, dialog string, prov core.Provenance) error {
	// Wrap extraction + embedding in a context timeout to prevent
	// goroutine leaks when LLM providers hang on socket reads.
	// The HTTP client has its own timeout, but this provides a
	// hard upper bound at the application level.
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

	// § 3.1: normalize once, here, so per-item vi.Store inside
	// processOneItemOnce writes a unit-length vector post-commit.
	// (vector.NormalizeVector is idempotent — a second pass inside
	// mergeEntityInTx / createEntityInTx is a no-op.)
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

// processOneItem handles dedup, contradiction resolution and atomic write for a single extracted entity.
//
// Wraps processOneItemOnce with a retry loop that absorbs transient
// SQLITE_BUSY ("database is locked") errors from db.BeginTx / tx.Commit
// under writer-side contention from the GC + parallel ingestion.
//
// Non-busy errors are returned immediately — retrying them would amplify
// the bug rather than fix it.
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
	return nil // unreachable; loop returns above
}

// viOpKind discriminates a per-entity vector-index operation applied
// AFTER a successful DB commit. The slice is built during the per-item
// decision phase (before BeginTx) and drained after itemTx.Commit()
// returns nil. Using a typed enum keeps the apply switch compile-time
// checked; a future `viOpSkip` half-step would land without changes
// to applyVIOps.
type viOpKind int

const (
	viOpStore viOpKind = iota
	viOpRemove
)

// viOp is one post-commit vector-index mutation. `vec` is only set for
// viOpStore operations; nil for viOpRemove.
type viOp struct {
	kind viOpKind
	id   string
	vec  []float32
}

// applyVIOps runs queued vi operations after itemTx.Commit() returned
// nil. Free function (not a method) because it depends only on the
// VectorIndex — making it a free `func(vi core.VectorIndex, ...)`
// expresses the actual dependency more accurately and matches the
// codebase style for pure-passthrough helpers (compare
// IsIngestionContradiction, isSQLiteBusyError).
//
// Each operation logs a WARN on failure (event=vi_drift) but does not
// surface the error: the DB is the source of truth and algo.ReEmbedAll
// can rebuild drift that accrues from cumulative post-commit vi
// failures. Fail-loud on the per-item path would mask successful
// ingest from downstream callers.
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

// processOneItemOnce is the unwrapped tx body. Do not call directly;
// always call processOneItem which retries on busy.
//
// § 3.1 atomicity contract:
//
//   - Every vi operation runs ONLY after itemTx.Commit() returns nil.
//   - The contradiction-archive UPDATE is folded INTO itemTx.
//   - Rollback-on-BeginTx-err / write-err / Commit-err calls no vi
//     operation: vi.Store only ever fires on successful commit, so no
//     vec mutation has occurred yet.
//
// viOps composition by decision branch:
//
//	NEW entity (no existing)        → [store(it.entity.ID, embedding)]
//	MERGE into existing             → [remove(it.entity.ID), store(mergeEntity.ID, mergeEntity.Embedding)]
//	LOW-CONF contradiction archive  → [remove(archiveIDFromExisting), store(it.entity.ID, embedding)]
//	HIGH-CONF contradiction (keep) → [store(it.entity.ID, embedding)]; existing.ID untouched because it was indexed by its prior ingest and the createEdgesInTx below adds a contradicts edge so the keep-both relation is durable.
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

// processInput bundles the extracted entity and its embedding.
type processInput struct {
	entity    core.ExtractedEntity
	embedding []float32
}

// executeItemTx runs the database transaction for creating or merging an entity.
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

// IsIngestionContradiction guards dedup by negation heuristic.
// The real implementation lives in detectors.LexicalDetector.
func IsIngestionContradiction(a, b string) bool {
	return detectors.NewLexicalDetector().Detect(
		core.Entity{Content: a},
		core.Entity{Content: b},
	).Detected
}

// MemoryWorker processes MemoryMessage channel items.
// Does NOT checkpoint work in-flight AND does NOT drain the channel
// buffer — use MemoryWorkerResilient for production ingest batches.
//
// Concurrency is bounded by a semaphore so a flooding producer
// cannot drive the worker into OOM or starve the SQLite
// single-writer (SetMaxOpenConns(1) in store.InitDB).
//
// On ctx.Done() the loop returns cleanly without leaving
// dangling goroutines: in-flight goroutines observe ctx through
// ProcessDialogWithProvenance and unwind themselves; the WaitGroup
// barriers the function exit.
func MemoryWorker(ctx context.Context, db *sql.DB, vi core.VectorIndex, extractor core.LLMExtractor, embedder core.Embedder, dedupThreshold float32, schema core.SchemaConfig, ch <-chan core.MemoryMessage) {
	worker := NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold, schema, detectors.NewLexicalDetector())
	// Sequential processing guarantees FIFO ordering — critical for
	// temporal consistency in playback and timeline queries.
	const maxParallel = 1
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

// MemoryWorkerResilientFromConfig is the production-grade ingest entry point.
// Supersedes MemoryWorkerResilient for any new caller.
func MemoryWorkerResilientFromConfig(ctx context.Context, cfg MemoryWorkerConfig, ch <-chan core.MemoryMessage) {
	worker := NewIngestionWorkerFromConfig(IngestionWorkerConfig{
		DB:             cfg.DB,
		VectorIndex:    cfg.VectorIndex,
		Extractor:      cfg.Extractor,
		Embedder:       cfg.Embedder,
		DedupThreshold: cfg.DedupThreshold,
		Schema:         cfg.Schema,
	})
	if cfg.CkptPath == "" && cfg.PendingPath == "" {
		slog.Warn("MemoryWorkerResilient: ckptPath and pendingPath both empty - no durability on cancel",
			"worker_id", cfg.WorkerID)
	}
	LoadCheckpoint(cfg.CkptPath, cfg.WorkerID)
	const maxParallel = 8
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	var processed atomic.Int64

	flushCheckpoint := func() {
		cur := IngestionCheckpoint{
			LastCommittedIndex: processed.Load(),
			LastCommittedAt:    time.Now().UTC(),
			WorkerID:           cfg.WorkerID,
		}
		if err := SaveCheckpoint(cfg.CkptPath, cur); err != nil {
			slog.Error("checkpoint save failed", "err", err, "path", cfg.CkptPath)
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
					"worker_id", cfg.WorkerID, "pending_count", len(pending))
				break drainLoop
			}
		}
		if err := SavePendingQueue(cfg.PendingPath, pending); err != nil {
			slog.Error("pending save failed", "err", err, "path", cfg.PendingPath)
		} else if len(pending) > 0 {
			slog.Info("MemoryWorkerResilient: drained to pending queue",
				"count", len(pending), "path", cfg.PendingPath)
		}
	}

	for {
		select {
		case <-ctx.Done():
			slog.Debug("MemoryWorkerResilient: ctx cancelled, draining", "worker_id", cfg.WorkerID)
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
				if err := worker.ProcessDialogWithProvenance(ctx, msg.Dialog, prov); err != nil {
					slog.Error("dialog processing failed",
						"err", err, "dialog_len", len(msg.Dialog), "worker_id", cfg.WorkerID)
					return
				}
				processed.Add(1)
				cur := IngestionCheckpoint{
					LastCommittedIndex: processed.Load(),
					LastCommittedAt:    time.Now().UTC(),
					WorkerID:           cfg.WorkerID,
				}
				if err := SaveCheckpoint(cfg.CkptPath, cur); err != nil {
					slog.Error("per-msg checkpoint save failed",
						"err", err, "index", cur.LastCommittedIndex)
				}
			}()
		}
	}
}

// MemoryWorkerResilient is the production-grade ingest entry point.
// Deprecated: Use MemoryWorkerResilientFromConfig instead.
//
//   - § 4.1 Checkpoint partial batches on ctx cancellation: after every
//     successful ProcessDialogWithProvenance return the worker atomically
//     persists an IngestionCheckpoint{LastCommittedIndex, LastCommittedAt,
//     WorkerID} to ckptPath via tmp+rename, so a restart can resume from
//     the last successful commit by skipping the first
//     LastCommittedIndex items in the producer's input stream.
//
//   - § 4.2 Drain the channel on ctx cancel: on ctx.Done() the worker
//     switches from dispatch mode to drain mode — reads any remaining
//     channel items into a side JSONL file (pendingPath) so the producer
//     can replay them on restart. The drain is bounded by a 5s
//     deadline (defaultDrainTimeout) so a producer that does not close
//     its channel cannot stall the worker indefinitely.
//
// Atomic checkpoint writes via os.Rename guarantee a concurrent reader
// can never observe a partially-flushed file. The `drain` and `wg.Wait`
// pair ensures no goroutine leak: in-flight goroutines observe ctx
// through ProcessDialogWithProvenance and unwind, and the WaitGroup
// barriers the function exit before the deferred-style cleanup returns.
//
// Empty ckptPath / pendingPath skip the corresponding persistence step
// — used by tests that don't need durable state.
func MemoryWorkerResilient(ctx context.Context, db *sql.DB, vi core.VectorIndex, extractor core.LLMExtractor, embedder core.Embedder, dedupThreshold float32, schema core.SchemaConfig, ckptPath, pendingPath, workerID string, ch <-chan core.MemoryMessage) {
	worker := NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold, schema, detectors.NewLexicalDetector())
	if ckptPath == "" && pendingPath == "" {
		slog.Warn("MemoryWorkerResilient: ckptPath and pendingPath both empty — no durability on cancel",
			"worker_id", workerID)
	}
	// LoadCheckpoint is invoked once for its operator-audit side effect
	// (logs WARN on missing/corrupt on-disk checkpoint). We deliberately
	// discard the returned struct value: every SaveCheckpoint call below
	// builds a fresh LOCAL IngestionCheckpoint so concurrent flusher
	// goroutines never race on a shared struct field.
	LoadCheckpoint(ckptPath, workerID)
	const maxParallel = 8
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	var processed atomic.Int64

	flushCheckpoint := func() {
		// Pair consistency is per-flush, NOT across simultaneous flushes.
		// Two goroutines may flush in parallel; the durable file content
		// is always one (LastCommittedIndex, LastCommittedAt) pair from
		// a single goroutine — never a torn interleave — because (a)
		// we build a LOCAL IngestionCheckpoint copy here (no shared
		// struct field is mutated across goroutines), AND (b)
		// SaveCheckpoint uses atomic-counter-unique tmp filenames +
		// POSIX-atomic os.Rename (see checkpoint.go).
		cur := IngestionCheckpoint{
			LastCommittedIndex: processed.Load(),
			LastCommittedAt:    time.Now().UTC(),
			WorkerID:           workerID,
		}
		if err := SaveCheckpoint(ckptPath, cur); err != nil {
			slog.Error("checkpoint save failed", "err", err, "path", ckptPath)
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
					"worker_id", workerID, "pending_count", len(pending))
				break drainLoop
			}
		}
		if err := SavePendingQueue(pendingPath, pending); err != nil {
			slog.Error("pending save failed", "err", err, "path", pendingPath)
		} else if len(pending) > 0 {
			slog.Debug("MemoryWorkerResilient: drained to pending queue",
				"count", len(pending), "path", pendingPath)
		}
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("MemoryWorkerResilient: ctx cancelled, draining", "worker_id", workerID)
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
				if err := worker.ProcessDialogWithProvenance(ctx, msg.Dialog, prov); err != nil {
					slog.Error("dialog processing failed",
						"err", err, "dialog_len", len(msg.Dialog), "worker_id", workerID)
					return
				}
				processed.Add(1)
				// Build a LOCAL IngestionCheckpoint copy so this goroutine
				// never races with another flushCheckpoint / per-msg call
				// on the same struct field. SaveCheckpoint copies the
				// struct again on entry so file content is internally
				// consistent.
				cur := IngestionCheckpoint{
					LastCommittedIndex: processed.Load(),
					LastCommittedAt:    time.Now().UTC(),
					WorkerID:           workerID,
				}
				if err := SaveCheckpoint(ckptPath, cur); err != nil {
					slog.Warn("per-msg checkpoint save failed",
						"err", err, "index", cur.LastCommittedIndex)
				}
			}()
		}
	}
}
