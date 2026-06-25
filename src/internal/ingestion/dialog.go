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
	"github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// ProcessDialog is the backward-compatible entry point.
func (w *IngestionWorker) ProcessDialog(ctx context.Context, dialog string) error {
	return w.ProcessDialogWithProvenance(ctx, dialog, core.Provenance{ExtractedFrom: dialog})
}

// ProcessDialogWithProvenance loads, embeds, and stores entities from one dialog.
//
// § 3.1 atomicity (round-9 refactor): every per-item vi (vector-index)
// operation runs ONLY AFTER w.db.BeginTx → itemTx.Commit() succeeds.
// BulkStore was REMOVED from this function — it used to write every
// extracted ID up-front, before any per-item tx, so a single tx
// failure could orphan vec entries for the still-unprocessed IDs in
// this dialog. The replacement path normalizes each embedding here
// (idempotent, fast) and lets processOneItemOnce write the vec entry
// after its own tx commits.
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
// § 3.1 atomicity contract (round-9 refactor):
//
//   - Every vi operation runs ONLY after itemTx.Commit() returns nil.
//   - The contradiction-archive UPDATE is folded INTO itemTx (was a
//     separate db.ExecContext outside the tx in the OLD code — that
//     ordering could leave the vec index with the archived entry
//     removed while the DB row stayed archived=0, i.e. SEARCH DRIFT).
//   - Rollback-on-BeginTx-err / write-err / Commit-err no longer calls
//     any vi operation: with BulkStore gone and vi.Store only ever
//     firing on successful commit, no vec mutation has occurred yet.
//     The previous vi.Remove(rollbackID) calls were no-ops that added
//     noise and could mislead a future reader about the atomicity
//     guarantee.
//
// viOps composition by decision branch:
//
//	NEW entity (no existing)        → [store(it.entity.ID, embedding)]
//	MERGE into existing             → [remove(it.entity.ID), store(mergeEntity.ID, mergeEntity.Embedding)]
//	LOW-CONF contradiction archive  → [remove(archiveIDFromExisting), store(it.entity.ID, embedding)]
//	HIGH-CONF contradiction (keep) → [store(it.entity.ID, embedding)]; existing.ID untouched because it was indexed by its prior ingest and the createEdgesInTx below adds a contradicts edge so the keep-both relation is durable.
func (w *IngestionWorker) processOneItemOnce(ctx context.Context, prov core.Provenance, it struct {
	entity    core.ExtractedEntity
	embedding []float32
}, similarIDs []string, selfID string) error {
	targetID := it.entity.ID
	existing, err := w.findMatch(it.embedding, similarIDs, selfID)
	if err != nil {
		return fmt.Errorf("entity match failed: %w", err)
	}

	var viOps []viOp
	// archiveID retains the existing.ID for the LOW-CONF contradiction
	// branch even after `existing = nil` clears the dedup handle. We
	// need the id both to fold the archive UPDATE INTO itemTx (atomic
	// with the new entity write) and to populate the post-commit Remove.
	var archiveID string
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
			archiveID = existing.ID
			// Post-commit Remove cleans the vec index; defensive against
			// any future bug reintroducing BulkStore-style pre-stores.
			viOps = append(viOps, viOp{kind: viOpRemove, id: existing.ID})
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
		// Merge-prep: defensive post-commit Remove drops any stale vec
		// entry for it.entity.ID from a prior aborted ingest; post-commit
		// Store writes the merged embedding. The OLD code did
		// vi.Remove(it.entity.ID) BEFORE BeginTx, which violated
		// atomicity — that Remove succeeded even when this tx rolled
		// back, leaving an orphan vec index entry.
		viOps = append(viOps,
			viOp{kind: viOpRemove, id: it.entity.ID},
			viOp{kind: viOpStore, id: mergeEntity.ID, vec: mergeEntity.Embedding},
		)
	} else {
		// Fresh entity: BulkStore is gone (§ 3.1), so the per-item
		// post-commit Store is the ONLY way the new ID lands in the
		// vec index. Without it, the new row would be invisible to
		// the next SearchBatch.
		viOps = append(viOps, viOp{kind: viOpStore, id: it.entity.ID, vec: it.embedding})
	}

	itemTx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx failed: %w", err)
	}
	var writeErr error
	if mergeEntity != nil {
		writeErr = w.mergeEntityInTx(ctx, itemTx, *mergeEntity)
	} else {
		writeErr = w.createEntityInTx(ctx, itemTx, it.entity, it.embedding, prov)
	}
	// Fold contradiction archive INTO itemTx so the archive commits /
	// rolls-back atomically with the new entity write. archiveID is
	// only non-empty here for the LOW-CONF contradiction case.
	if archiveID != "" && writeErr == nil {
		if _, err := itemTx.ExecContext(ctx, `UPDATE entities SET archived = 1 WHERE id = ?`, archiveID); err != nil {
			writeErr = fmt.Errorf("archive existing in tx: %w", err)
		}
	}
	if writeErr == nil {
		writeErr = w.createEdgesInTx(ctx, itemTx, targetID, it.entity.Relations)
	}
	if writeErr != nil {
		_ = itemTx.Rollback()
		return writeErr
	}
	if err := itemTx.Commit(); err != nil {
		return err
	}
	// COMMIT SUCCESSFUL — drain vi ops. Vec failures are logged but
	// do not fail the item: DB is the source of truth and algo.ReEmbedAll
	// can rebuild any drift that accrues from cumulative vec failures.
	applyVIOps(ctx, w.vi, viOps)
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

// IsIngestionContradiction guards dedup by negation heuristic. Retained
// as a free function so existing callers/tests stay green; the real
// implementation now lives in contradiction.LexicalDetector.
func IsIngestionContradiction(a, b string) bool {
	return contradiction.NewLexicalDetector().Detect(
		core.Entity{Content: a},
		core.Entity{Content: b},
	).Detected
}

// MemoryWorker processes MemoryMessage channel items — legacy entry
// point, retained for back-compat with any external consumer that
// wired the parameter list before MemoryWorkerResilient shipped.
// Does NOT checkpoint work in-flight AND does NOT drain the channel
// buffer — use MemoryWorkerResilient for production ingest batches.
//
// Status as of round-8 (TODO § 4 closure): both MemoryWorker AND
// MemoryWorkerResilient have ZERO in-tree callers. Verify with:
// `grep -rnF MemoryWorker src/internal/ | grep -v _test.go`
// — expected hits are exactly the two `^func` definitions in
// this file. The DEADCODE reservation that motivated the previous
// annotation is now satisfied — both functions ship side-by-side
// so a future caller can pick the right shape without a forced
// migration.
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

// MemoryWorkerResilient is the production-grade ingest entry point —
// supersedes MemoryWorker for any new caller. Two behaviour changes:
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
	worker := NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold, schema)
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
			slog.Error("pending save save failed", "err", err, "path", pendingPath)
		} else if len(pending) > 0 {
			slog.Info("MemoryWorkerResilient: drained to pending queue",
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
					slog.Error("per-msg checkpoint save failed",
						"err", err, "index", cur.LastCommittedIndex)
				}
			}()
		}
	}
}
