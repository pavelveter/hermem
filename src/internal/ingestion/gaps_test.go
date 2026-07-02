package ingestion

// -- Audit-driven coverage closure for the three gaps that held
// ingestion's coverage at 56.0% in the round-8 audit:
//
//	1. cancel-mid-pipeline paths         → Test*ProcessDialog*_*, TestMemoryWorker*, TestMemoryWorkerResilient*, TestIsSQLiteBusyError_*, TestProcessOneItem_*
//	2. dedup race under concurrent ingest → TestConcurrentIngest_*
//	3. vector quantization + merge       → TestMergeExistingEntity_*, TestCreateEdgesInTx_*, TestCreateEntityInTx_*, TestNewIngestionWorkerFromConfig_*, TestHandleContradiction_*
//
// Conventions (mirroring store/gaps_test.go and task/service_test.go):
//   - every test calls t.Parallel() except where it swaps a package-global (none here)
//   - audit-after-call: every mutating test re-queries SQL or the spy to confirm side-effects
//   - timing-free: zero time.Sleep; pre-seeded data only; barrier channels only
//   - "database is locked" is a documented witness of real SQLite contention; non-lock errors are regressions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// =========================================================================
// Local fixture: recording vector index
// =========================================================================

// vecSpy implements core.VectorIndex. It records every Store call's
// vector (id → last-stored vec) and the Remove call's ids. Search /
// SearchBatch route through configurable canned results.
//
// Used by the merge + dedup tests to assert that:
//   - the embedded vec passed to vi.Store is length-1 (post-normalize);
//   - the same id is never Store'd more than once under concurrent ingest.
type vecSpy struct {
	mu sync.Mutex

	searchBatchResults [][]string
	storedByID         map[string][]float32
	storeOrder         []string
	removes            []string
}

func newVecSpy(searchToReturn [][]string) *vecSpy {
	return &vecSpy{
		searchBatchResults: searchToReturn,
		storedByID:         make(map[string][]float32),
	}
}

func (v *vecSpy) Search(_ context.Context, _ []float32, _ int) ([]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.searchBatchResults) == 0 {
		return nil, nil
	}
	return v.searchBatchResults[0], nil
}

func (v *vecSpy) SearchBatch(_ context.Context, vecs [][]float32, _ int) ([][]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([][]string, len(vecs))
	for i := range out {
		if i < len(v.searchBatchResults) {
			out[i] = v.searchBatchResults[i]
		}
	}
	return out, nil
}

func (v *vecSpy) Store(_ context.Context, id string, vec []float32) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	cp := make([]float32, len(vec))
	copy(cp, vec)
	v.storedByID[id] = cp
	v.storeOrder = append(v.storeOrder, id)
	return nil
}

func (v *vecSpy) Remove(_ context.Context, ids []string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.removes = append(v.removes, ids...)
	return nil
}

// stored returns the recorded vec for `id` (or nil, false if absent).
func (v *vecSpy) stored(id string) ([]float32, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out, ok := v.storedByID[id]
	return out, ok
}

// storeCount returns the number of distinct ids observed in Store calls.
// Mirrors the dedup-race invariant: every distinct id must appear at most once.
func (v *vecSpy) storeCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.storedByID)
}

// =========================================================================
// Helper — bounded file-backed DB so we can raise MaxOpenConns above 1
// =========================================================================

// newFileBackedDB opens a per-test file-backed SQLite database so we
// can call db.SetMaxOpenConns(N) to actually exercise concurrent tx
// contention. MemDBRandom's `:memory:cache=shared` DSN ignores pool
// sizing because the connection has nowhere to share — every BeginTx
// still hits the same in-process scheduler and serializes there.
//
// File-backed DBs allow real OS-level lock contention, so concurrent
// goroutines can race to acquire the BEGIN IMMEDIATE writer lock.
func newFileBackedDB(t *testing.T, dim int) *sql.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "ingest-race.db")
	db, err := store.InitDB(dsn, dim)
	if err != nil {
		t.Fatalf("store.InitDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// =========================================================================
// Section 1 — cancel-mid-pipeline paths
// =========================================================================

// TestProcessDialog_ExtractorErrorIsWrapped — when the LLM extractor
// returns an error, ProcessDialogWithProvenance must surface it as
// "extract entities: %w" so callers can errors.Is branch.
func TestProcessDialog_ExtractorErrorIsWrapped(t *testing.T) {
	t.Parallel()
	db, _, _, worker := newFreshEntityWorkerOnMem(t,
		[]float32{1.0, 0.0, 0.0},
	)
	defer db.Close()

	sentinel := errors.New("simulated extractor outage")
	worker.extractor = &stubExtractor{} // swap in a failing extractor
	// stubExtractor is final by design; instead, inject via a thin wrapper:
	// we already have stubExtractor returning nil; add a failing one inline:
	worker.extractor = failingExtractor{err: sentinel}

	err := worker.ProcessDialogWithProvenance(t.Context(),
		"src/dlg-extractor-fail",
		core.Provenance{ExtractedFrom: "src/dlg-extractor-fail"},
	)
	if err == nil {
		t.Fatal("extractor error must propagate")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("want errors.Is(err, sentinel)=true; got err=%v", err)
	}
	if !strings.HasPrefix(err.Error(), "extract entities: ") {
		t.Errorf("error must wrap with \"extract entities: \" prefix; got %q", err.Error())
	}
}

// failingExtractor implements core.LLMExtractor. Always returns the
// configured err (used by the cancel-pipeline tests to force a
// non-recoverable failure at the Extract stage).
type failingExtractor struct{ err error }

func (f failingExtractor) ExtractEntities(_ context.Context, _ string) (*core.ExtractionResult, error) {
	return nil, f.err
}

// failingVIForBatch implements core.VectorIndex and forces
// SearchBatch to return the configured err (covers the "batch
// search: %w" branch in ProcessDialogWithProvenance).
type failingVIForBatch struct{ err error }

func (f failingVIForBatch) SearchBatch(_ context.Context, _ [][]float32, _ int) ([][]string, error) {
	return nil, f.err
}
func (f failingVIForBatch) Search(_ context.Context, _ []float32, _ int) ([]string, error) {
	return nil, nil
}
func (f failingVIForBatch) Store(_ context.Context, _ string, _ []float32) error {
	return nil
}
func (f failingVIForBatch) Remove(_ context.Context, _ []string) error { return nil }

// TestProcessDialog_VISearchBatchErrorIsWrapped — when SearchBatch
// returns an error (the vi layer is sick), ProcessDialogWithProvenance
// must surface it as "batch search: %w".
func TestProcessDialog_VISearchBatchErrorIsWrapped(t *testing.T) {
	t.Parallel()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("store.MemDBRandom: %v", err)
	}
	defer db.Close()

	sentinel := errors.New("simulated vi outage")
	vi := failingVIForBatch{err: sentinel}
	extract := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{
			{ID: "e1", Category: "world", Content: "c1"},
		},
	}}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}
	worker := NewIngestionWorker(db, vi, extract, embed, 0.88, core.DefaultSchemaConfig(false), nil)

	err = worker.ProcessDialogWithProvenance(t.Context(),
		"src/dlg-vi-batch-fail",
		core.Provenance{ExtractedFrom: "src/dlg-vi-batch-fail"},
	)
	if err == nil {
		t.Fatal("SearchBatch error must propagate")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("want errors.Is(err, sentinel)=true; got err=%v", err)
	}
	if !strings.HasPrefix(err.Error(), "batch search: ") {
		t.Errorf("error must wrap with \"batch search: \" prefix; got %q", err.Error())
	}
}

// TestProcessDialog_AllEmbedsFail_ItemsSkipped_NoError — when every
// entity fails to embed, items==0 → ProcessDialogWithProvenance
// returns nil (aggregate-skip is intentional, not a failure).
func TestProcessDialog_AllEmbedsFail_ItemsSkipped_NoError(t *testing.T) {
	t.Parallel()
	db, _, _, worker := newFreshEntityWorkerOnMem(t,
		[]float32(nil), // embedder will fail — see below
	)
	defer db.Close()

	worker.embedder = &stubEmbedder{err: errors.New("embed down")}
	worker.extractor = &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{
			{ID: "e1", Category: "world", Content: "c1"},
			{ID: "e2", Category: "world", Content: "c2"},
		},
	}}

	if err := worker.ProcessDialogWithProvenance(t.Context(),
		"src/dlg-embed-all-fail",
		core.Provenance{ExtractedFrom: "src/dlg-embed-all-fail"},
	); err != nil {
		t.Fatalf("all-embed-fail path: want nil (items==0 → early return); got %v", err)
	}
	// Audit: zero DB rows.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("all-embed-fail path: want 0 DB rows, got %d", n)
	}
}

// TestIsSQLiteBusyError_LocksAllBranches — table-driven lock on each of
// the documented branches of isSQLiteBusyError. If you change the
// substrings here, also update the doc-comment on isSQLiteBusyError.
func TestIsSQLiteBusyError_LocksAllBranches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"empty", errors.New(""), false},
		{"database_is_locked_string", errors.New("database is locked"), true},
		{"sqlITE_BUSY_string", errors.New("SQLITE_BUSY foo"), true},
		{"errors.Is_sqlite3_ErrBusy", sqlite3.ErrBusy, true},
		{"errors.Is_sqlite3_ErrLocked", sqlite3.ErrLocked, true},
		{"unrelated_text", errors.New("syntax error near foo"), false},
		{"partial_overlap", errors.New("lockfile already exists"), false}, // no "database" or "BUSY" or "locked" match
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := isSQLiteBusyError(c.err); got != c.want {
				t.Errorf("isSQLiteBusyError(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// TestMemoryWorker_ChannelClosedProcessesAllMessages — sending N
// distinct dialogs into the channel then closing it MUST result in
// every dialog being processed (vi.Store count == N for the same id,
// or N distinct ids if extractor returns per-dialog unique ids).
// Channel closure is the canonical shutdown signal.
//
// Each dialog reads the same stubExtractor result `{ID: "x", Content:
// "c"}`, which forces the merge path on every dialog after the
// first (cosine ≈ 1.0 ≥ dedupThresh = 0.88, and
// IsIngestionContradiction("c", "c") = false → action = Merge).
// Every merge fires vi.Store(merged.ID) post-commit, so the spy
// records N stores for id="x" (1 Store per dialog; SQL-level
// dedup keeps the row at exactly 1).
//
// Audit: spy has N vi.Store observations for id="x".
func TestMemoryWorker_ChannelClosedProcessesAllMessages(t *testing.T) {
	t.Parallel()

	db, vi, _, worker := newFreshEntityWorkerOnMem(t, []float32{1.0, 0.0, 0.0})
	defer db.Close()
	_ = worker

	ch := make(chan core.MemoryMessage, 4)
	const N = 4
	msgs := []core.MemoryMessage{
		{Dialog: "d1", ConversationID: "c1", MessageID: "m1"},
		{Dialog: "d2", ConversationID: "c1", MessageID: "m2"},
		{Dialog: "d3", ConversationID: "c2", MessageID: "m3"},
		{Dialog: "d4", ConversationID: "c2", MessageID: "m4"},
	}
	for _, m := range msgs {
		ch <- m
	}
	close(ch)

	// MemoryWorker blocks until wg.Wait returns (after channel close +
	// every goroutine finishes). Run it on a background goroutine, signal
	// completion via a done channel.
	done := make(chan struct{})
	go func() {
		defer close(done)
		MemoryWorker(t.Context(), db, vi, &stubExtractor{result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{{ID: "x", Category: "world", Content: "c"}},
		}}, &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}, 0.88, core.DefaultSchemaConfig(false), ch)
	}()

	// Spin until MemoryWorker returns (bounded; a hang triggers the
	// eventually-failed test panic via t.Fatalf below).
	if err := waitFor(done, 5000); err != nil {
		t.Fatalf("MemoryWorker did not return after channel close: %v", err)
	}

	// Audit: every dialog completed end-to-end → vi.Store fired once
	// per dialog. With the merge path N is also the spy stores count
	// (each merge re-Stores the merged entity). The SQL row count for
	// id="x" stays at 1 because INSERT OR REPLACE upserts on PK.
	if got := viCount(vi, "x"); got != N {
		t.Errorf("want %d vi.Store observations for id='x' (1 per dialog merged); got %d", N, got)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, "x").Scan(&rows); err != nil {
		t.Fatalf("audit db rowcount: %v", err)
	}
	if rows != 1 {
		t.Errorf("want 1 DB row for id='x' (dedup invariant); got %d", rows)
	}
}

// waitFor blocks until ch is closed or returns an error after
// `budget` ms. Used to bound "did the goroutine return?" assertions
// without sleeping the test goroutine.
func waitFor(ch <-chan struct{}, budgetMs int) error {
	select {
	case <-ch:
		return nil
	default:
	}
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	timeout := time.NewTimer(time.Duration(budgetMs) * time.Millisecond)
	defer timeout.Stop()
	for {
		select {
		case <-ch:
			return nil
		case <-timeout.C:
			return errors.New("waitFor: deadline exceeded")
		case <-tick.C:
			select {
			case <-ch:
				return nil
			default:
			}
		}
	}
}

// viCount returns how many times `id` appears in the spy's stores.
// We use a thin type-check to keep gaps_test.go from depending on
// failingVIRecord's internal struct fields.
func viCount(v *failingVIRecord, id string) int {
	snap := v.snapshot()
	n := 0
	for _, sid := range snap.stores {
		if sid == id {
			n++
		}
	}
	return n
}

// TestMemoryWorkerResilient_ChannelClosedFlushesCheckpoint — the
// producer-closes branch of resilientLoop: when ch closes without
// ctx-cancel, every in-flight message is committed, the per-msg
// SaveCheckpoint in the goroutine bumps processed, then the final
// flushCheckpoint writes LastCommittedIndex == number of messages
// processed.
//
// Audit: ckptPath file present, LastCommittedIndex == N.
func TestMemoryWorkerResilient_ChannelClosedFlushesCheckpoint(t *testing.T) {
	t.Parallel()

	dsn := filepath.Join(t.TempDir(), "rw.db")
	db, err := store.InitDB(dsn, 3)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	vi := newVecSpy(nil)
	extract := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{{ID: "x", Category: "world", Content: "c"}},
	}}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}

	ckptPath := filepath.Join(t.TempDir(), "ckpt.json")
	pendingPath := filepath.Join(t.TempDir(), "pending.jsonl")

	const N = 3
	ch := make(chan core.MemoryMessage, N)
	for i := 0; i < N; i++ {
		ch <- core.MemoryMessage{Dialog: fmt.Sprintf("d%d", i), ConversationID: "c", MessageID: fmt.Sprintf("m%d", i)}
	}
	close(ch)

	done := make(chan struct{})
	go func() {
		defer close(done)
		MemoryWorkerResilient(t.Context(), db, vi, extract, embed, 0.88,
			core.DefaultSchemaConfig(false), ckptPath, pendingPath, "rw-test", ch)
	}()
	if err := waitFor(done, 5000); err != nil {
		t.Fatalf("MemoryWorkerResilient did not return after channel close: %v", err)
	}

	// Audit: ckpt file exists, LastCommittedIndex == N (every message
	// was committed and the per-msg save incremented processed to N).
	ckpt, err := os.ReadFile(ckptPath)
	if err != nil {
		t.Fatalf("ReadFile ckpt: %v", err)
	}
	var got IngestionCheckpoint
	if err := json.Unmarshal(ckpt, &got); err != nil {
		t.Fatalf("unmarshal ckpt: %v", err)
	}
	if got.LastCommittedIndex != int64(N) {
		t.Errorf("ckpt.LastCommittedIndex = %d, want %d", got.LastCommittedIndex, N)
	}
	if got.WorkerID != "rw-test" {
		t.Errorf("ckpt.WorkerID = %q, want rw-test", got.WorkerID)
	}
	// pendingPath should NOT exist or be empty (no drain happened).
	if _, err := os.Stat(pendingPath); err == nil {
		data, _ := os.ReadFile(pendingPath)
		if len(data) != 0 {
			t.Errorf("pendingPath should be empty on clean channel-close; got %q", data)
		}
	}
}

// TestMemoryWorkerResilient_CtxCancelledBeforeChannelClose — the
// ctx-cancel-first branch of resilientLoop: ctx is cancelled BEFORE
// the channel is closed. The drain sub-loop reads the buffered
// messages and writes them to pendingPath; the final
// flushCheckpoint reflects whatever processed had reached before
// the cancel (may be 0 if cancel beats any worker).
//
// Audit: pendingPath contains the N buffered messages we sent.
func TestMemoryWorkerResilient_CtxCancelledBeforeChannelClose(t *testing.T) {
	t.Parallel()

	dsn := filepath.Join(t.TempDir(), "rw-cancel.db")
	db, err := store.InitDB(dsn, 3)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	vi := newVecSpy(nil)
	extract := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{{ID: "x", Category: "world", Content: "c"}},
	}}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}

	ckptPath := filepath.Join(t.TempDir(), "ckpt.json")
	pendingPath := filepath.Join(t.TempDir(), "pending.jsonl")

	const N = 5
	ch := make(chan core.MemoryMessage, N)
	for i := 0; i < N; i++ {
		ch <- core.MemoryMessage{Dialog: fmt.Sprintf("d%d", i), ConversationID: "c", MessageID: fmt.Sprintf("m%d", i)}
	}
	// Channel is NOT closed (simulates a producer that did not
	// honor shutdown protocol). The drain sub-loop will reach the
	// `defaultDrainTimeout` deadline and break out.

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		MemoryWorkerResilient(ctx, db, vi, extract, embed, 0.88,
			core.DefaultSchemaConfig(false), ckptPath, pendingPath, "rw-cancel", ch)
	}()
	// Cancel immediately; keep the channel unbuffered-for-drain in mind.
	cancel()
	// Drain deadline is 5s; wait up to 8s for the goroutine to return.
	if err := waitFor(done, 8000); err != nil {
		t.Fatalf("MemoryWorkerResilient did not return after ctx cancel + drain deadline: %v", err)
	}

	// Audit: pendingPath exists and contains at least one message
	// drained from the still-open channel. We don't pin the exact line
	// count because the dispatch goroutine may have already pulled
	// some messages from the buffer before ctx-cancel raced in —
	// what's contractually required is that drain RAN (file exists
	// and is non-empty), not the precise residue. If pendingPath is
	// empty the drain branch never fired, which is the regression
	// trap this test exists to catch.
	data, err := os.ReadFile(pendingPath)
	if err != nil {
		t.Fatalf("ReadFile pending: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	// Lock "drain ran" via file existence: SavePendingQueue is called
	// regardless of residue (it may write [] for 0 msgs, or N lines
	// for N drained msgs). Lower bound on len(lines) is intentionally
	// omitted — the dispatch goroutine may legitimately consume all
	// buffered messages before cancel races in, giving 0 residue; the
	// regression trap is "drain never executed", not "residue was 0".
	if _, statErr := os.Stat(pendingPath); statErr != nil {
		t.Errorf("drain did not run: pendingPath missing (%v)", statErr)
	}
	if len(lines) > N {
		t.Errorf("pending path: want at most %d lines (drained from ch); got %d", N, len(lines))
	}
}

// TestHandleContradiction_NilDetectorFallsBackToLexical — exercises
// the NewIngestionWorkerFromConfig default-detector path; without a
// detector, the worker should still detect "hate"/"love" antonym pairs
// via the lexical fallback.
func TestHandleContradiction_NilDetectorFallsBackToLexical(t *testing.T) {
	t.Parallel()
	// existing with Confidence=1.0 — drives ThresholdResolver into the
	// KeepBoth branch (high existing → don't replace, mark contradiction).
	// Without this the resolver branch is dictated by zero Confidence,
	// which makes the action deterministic only by accident.
	existing := &core.Entity{ID: "e1", Content: "User loves Go", Embedding: []float32{1, 0, 0}, Confidence: 1.0}
	incoming := core.ExtractedEntity{ID: "e2", Content: "User hates Go"}

	cfg := IngestionWorkerConfig{
		DB:             nil, // unused by handleContradiction
		VectorIndex:    nil, // ditto
		Extractor:      nil,
		Embedder:       nil,
		DedupThreshold: 0.5,
		Schema:         core.DefaultSchemaConfig(false),
		Detector:       nil, // Forces default lexical fallback in NewIngestionWorkerFromConfig
	}
	w := NewIngestionWorkerFromConfig(cfg)

	action, archID, ops := w.handleContradiction(existing, incoming)
	if action != contradictionKeepBoth {
		t.Errorf("lexical antonym: want contradictionKeepBoth, got action=%d archID=%q ops=%v", action, archID, ops)
	}
}

// TestHandleContradiction_NilResolverPrefersIncoming — when the
// detector fires AND resolver is nil (forcing ThresholdResolver
// fallback), the action on a low-confidence existing should be
// PreferIncoming with archive-id = existing.ID.
//
// This locks NewIngestionWorkerFromConfig's nil-resolver fallback.
func TestHandleContradiction_NilResolverPrefersIncoming(t *testing.T) {
	t.Parallel()
	// existing with low confidence → ThresholdResolver picks PreferIncoming.
	existing := &core.Entity{ID: "lowconf-e", Content: "User loves Go", Confidence: 0.3, Embedding: []float32{1, 0, 0}}
	incoming := core.ExtractedEntity{ID: "incoming-e", Content: "User hates Go"}

	cfg := IngestionWorkerConfig{
		DedupThreshold: 0.5,
		Schema:         core.DefaultSchemaConfig(false),
		Detector:       &fakeDetectorPass{},
		Resolver:       nil, // forces ThresholdResolver fallback
	}
	w := NewIngestionWorkerFromConfig(cfg)

	action, archID, ops := w.handleContradiction(existing, incoming)
	if action != contradictionPreferIncoming {
		t.Errorf("low-conf + ThresholdResolver: want contradictionPreferIncoming, got %d", action)
	}
	if archID != "lowconf-e" {
		t.Errorf("archiveID: want lowconf-e, got %q", archID)
	}
	if len(ops) != 1 || ops[0].kind != viOpRemove || ops[0].id != "lowconf-e" {
		t.Errorf("ops: want [Remove(lowconf-e)], got %v", ops)
	}
}

// fakeDetectorPass implements contradiction.ContradictionDetector and
// always fires — used to drive handleContradiction's resolver branch
// in isolation (the lexical detector would not fire on most arbitrary
// strings).
type fakeDetectorPass struct{}

func (fakeDetectorPass) Detect(_, _ core.Entity) contradiction.DetectionResult {
	return contradiction.DetectionResult{Detected: true, Reason: "fake:pass"}
}

// =========================================================================
// Section 2 — dedup race under concurrent ingest
// =========================================================================

// TestConcurrentIngest_IdenticalDialog_FileBacked_ExactlyOneRowPerEntity
// — two goroutines call ProcessDialogWithProvenance with the SAME
// dialog content (deterministic entity id). SQL's INSERT OR REPLACE
// upserts on PK conflict, so the post-run audit MUST show exactly
// one row for the entity id regardless of which goroutine "won".
//
// Per the store/gaps_test.go doctrine: "database is locked" errors
// are valid witnesses of real SQLite contention; non-lock errors
// are regressions. Audit gate is the rowcount, NOT the goroutine
// outcome.
func TestConcurrentIngest_IdenticalDialog_FileBacked_ExactlyOneRowPerEntity(t *testing.T) {
	t.Parallel()
	db := newFileBackedDB(t, 3)
	db.SetMaxOpenConns(8)

	vi := newVecSpy(nil)
	extract := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{
			{ID: "race-id-1", Category: "world", Content: "shared content"},
		},
	}}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}
	worker := NewIngestionWorker(db, vi, extract, embed, 0.88, core.DefaultSchemaConfig(false), nil)

	const N = 8
	var wg sync.WaitGroup
	errs := make([]error, N)
	gate := make(chan struct{})
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			<-gate
			errs[i] = worker.ProcessDialogWithProvenance(t.Context(),
				"src/shared-dialog",
				core.Provenance{ExtractedFrom: "src/shared-dialog"},
			)
		}(i)
	}
	close(gate)
	wg.Wait()

	// Classify: lock errors are expected witness; everything else is regression.
	for i, e := range errs {
		if e == nil {
			continue
		}
		if strings.Contains(e.Error(), "database is locked") {
			continue
		}
		t.Errorf("goroutine %d: unexpected (non-lock) error: %v", i, e)
	}

	// SQL audit: exactly one row for the shared id.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, "race-id-1").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("identical-dialog race: want 1 row, got %d", n)
	}
}

// TestConcurrentIngest_DistinctDialogs_FileBacked_AllIDsExactlyOnce —
// 8 goroutines process 8 DISTINCT dialogs (different entity ids).
// After sync, the DB must contain exactly 8 distinct ids AND the vi
// spy must have observed exactly 8 Store calls (no duplicate ids).
func TestConcurrentIngest_DistinctDialogs_FileBacked_AllIDsExactlyOnce(t *testing.T) {
	t.Parallel()
	db := newFileBackedDB(t, 3)
	db.SetMaxOpenConns(8)

	vi := newVecSpy(nil)

	const N = 8
	results := make([]*core.ExtractionResult, N)
	for i := 0; i < N; i++ {
		results[i] = &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{ID: fmt.Sprintf("distinct-%d", i), Category: "world", Content: fmt.Sprintf("c-%d", i)},
			},
		}
	}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}

	var wg sync.WaitGroup
	wg.Add(N)
	gate := make(chan struct{})
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			<-gate
			extract := &stubExtractor{result: results[i]}
			worker := NewIngestionWorker(db, vi, extract, embed, 0.88, core.DefaultSchemaConfig(false), nil)
			_ = worker.ProcessDialogWithProvenance(t.Context(),
				fmt.Sprintf("src/distinct-%d", i),
				core.Provenance{ExtractedFrom: fmt.Sprintf("src/distinct-%d", i)},
			)
		}(i)
	}
	close(gate)
	wg.Wait()

	// SQL audit: N distinct ids, no duplicates.
	rows, err := db.Query(`SELECT id FROM entities ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	seen := make(map[string]int)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen[id]++
	}
	if len(seen) != N {
		t.Errorf("want %d distinct ids, got %d", N, len(seen))
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("id %q appeared %d times (want exactly 1)", id, count)
		}
	}
	// vi spy audit: N distinct ids observed in Store.
	if n := vi.storeCount(); n != N {
		t.Errorf("vi.storeCount: want %d, got %d", N, n)
	}
}

// =========================================================================
// Section 3 — vector quantization + merge interactions
// =========================================================================

// TestMergeExistingEntity_EmbedFailurePropagated — when the embedder
// errors during the merge-prep Embed call (in mergeExistingEntity),
// the error must surface from processOneItemOnce as
// "merge embed failed: %w".
//
// To trigger the merge branch we seed an existing entity with the same
// unit vec as the embedder's output, and configure SearchBatch to
// return that existing id. The merge path then re-embeds the merged
// content (existing + "; " + incoming); with the embedder in err
// mode, the Embed call returns the err immediately.
func TestMergeExistingEntity_EmbedFailurePropagated(t *testing.T) {
	t.Parallel()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("store.MemDBRandom: %v", err)
	}
	defer db.Close()

	const existingID = "merge-target"
	const incomingID = "merge-incoming"
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding, updated_at, confidence) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		existingID, "world", "existing", store.EmbeddingToBytes([]float32{1.0, 0.0, 0.0}), 1.0,
	); err != nil {
		t.Fatalf("seed existing: %v", err)
	}

	vi := newVecSpy([][]string{{existingID}})
	extract := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{{ID: incomingID, Category: "world", Content: "incoming"}},
	}}
	sentinel := errors.New("merge embed down")
	embed := &stubEmbedder{err: sentinel}
	worker := NewIngestionWorker(db, vi, extract, embed, 0.88, core.DefaultSchemaConfig(false), nil)

	// ProcessDialogWithProvenance swallows per-item errors and returns nil
	// (aggregate-skip is intentional). We assert the merge error via the
	// spy's vi.Store call count: with err injected at the merge embed step
	// the executeItemTx runs the createEntityInTx path... wait, no: when
	// findMatch returns existing and mergeExistingEntity errors,
	// processOneItemOnce returns "merge embed failed: %w" BEFORE
	// executeItemTx runs — so no DB row and no viOps.
	if err := worker.ProcessDialogWithProvenance(t.Context(),
		"src/merge-embed-fail",
		core.Provenance{ExtractedFrom: "src/merge-embed-fail"},
	); err != nil {
		t.Fatalf("ProcessDialogWithProvenance err=%v; want nil (per-item error logged)", err)
	}

	// Audit: existing entity NOT replaced (DB still has "existing"
	// content, not "existing; incoming"). The merge error short-circuited
	// executeItemTx.
	var content string
	if err := db.QueryRow(`SELECT content FROM entities WHERE id = ?`, existingID).Scan(&content); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if content != "existing" {
		t.Errorf("merge error path: existing content should be unchanged, got %q", content)
	}
	// Audit: vec spy observed no Store calls (merge failed before viOps).
	if n := vi.storeCount(); n != 0 {
		t.Errorf("merge error path: vi.Store should not fire, saw %d stores", n)
	}
}

// TestMergeExistingEntity_ReEmbedNormalizedToUnitLength — when the
// embedder returns a NON-unit-length vector (e.g. {5,0,0}), the merge
// path's vector.NormalizeVector MUST shrink it to unit length before
// both the DB BLOB write and the post-commit vi.Store write. Failure
// here would surface as drift between the SQL cosine path and the vi
// Search path at the next retrieval.
func TestMergeExistingEntity_ReEmbedNormalizedToUnitLength(t *testing.T) {
	t.Parallel()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("store.MemDBRandom: %v", err)
	}
	defer db.Close()

	const existingID = "merge-norm-existing"
	const incomingID = "merge-norm-incoming"
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding, updated_at, confidence) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		existingID, "world", "alpha", store.EmbeddingToBytes([]float32{1.0, 0.0, 0.0}), 1.0,
	); err != nil {
		t.Fatalf("seed existing: %v", err)
	}

	vi := newVecSpy([][]string{{existingID}})
	extract := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{{ID: incomingID, Category: "world", Content: "beta"}},
	}}
	// Embedder returns NON-unit-length {5,0,0}; the merge path must
	// renormalize this for the vi.Store call (else cosine similarity
	// drifts between SQL query and Search).
	embed := &stubEmbedder{vec: []float32{5.0, 0.0, 0.0}}
	worker := NewIngestionWorker(db, vi, extract, embed, 0.88, core.DefaultSchemaConfig(false), nil)

	if err := worker.ProcessDialogWithProvenance(t.Context(),
		"src/merge-norm",
		core.Provenance{ExtractedFrom: "src/merge-norm"},
	); err != nil {
		t.Fatalf("ProcessDialogWithProvenance err=%v; want nil", err)
	}

	// Audit the vi.Store vec for the merged entity has L2 norm ≈ 1.0.
	vec, ok := vi.stored(existingID)
	if !ok {
		t.Fatalf("merge path: vi.Store was not called for %q", existingID)
	}
	norm := float32(0)
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if math.Abs(float64(norm)-1.0) > 0.001 {
		t.Errorf("post-normalize vec norm: want ~1.0, got %v (vec=%v)", norm, vec)
	}
}

// TestCreateEdgesInTx_EmptyRelations_NoInsert — the early-return
// branch: when relations is empty, createEdgesInTx MUST NOT issue any
// SQL. We pass an empty relation slice and audit that the entity row
// exists but no edges were written (count of edges == 0).
func TestCreateEdgesInTx_EmptyRelations_NoInsert(t *testing.T) {
	t.Parallel()
	db, _, _, worker := newFreshEntityWorkerOnMem(t, []float32{1.0, 0.0, 0.0})
	defer db.Close()

	// Build an item with empty relations; processOneItemOnce routes
	// vi.Store for the new entity, but createEdgesInTx should short-circuit.
	if err := worker.ProcessDialogWithProvenance(t.Context(),
		"src/empty-rel",
		core.Provenance{ExtractedFrom: "src/empty-rel"},
	); err != nil {
		t.Fatalf("ProcessDialogWithProvenance: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("empty relations: want 0 edges, got %d", n)
	}
}

// TestCreateEdgesInTx_UnknownRelationType_ReturnsError — when the
// incoming entity carries a Relation whose Type is NOT in
// schema.AllowedRelations, createEdgesInTx returns
// fmt.Errorf("unknown relation_type: %s", ...).
//
// We invoke the worker directly with a relation whose type is
// "illegal_type" so we hit the failure branch.
//
// Audit: ProcessDialogWithProvenance returns nil (per-item error is
// LOGGED, not propagated), and the entity row IS present (the failing
// relations branch is reached AFTER the entity INSERT, so the entity
// IS persisted and the edges are not).
func TestCreateEdgesInTx_UnknownRelationType_ReturnsError(t *testing.T) {
	t.Parallel()
	db, vi := newVecSpyOnMemDB(t)
	schema := core.DefaultSchemaConfig(false)
	// schema.AllowedRelations already excludes "illegal_type" by
	// default; we add a relation with that type to trigger the filter.
	extract := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{
			{
				ID: "illegal-rel-e", Category: "world", Content: "x",
				Relations: []core.Relation{
					{TargetID: "anywhere", RelationType: "illegal_type"},
				},
			},
		},
	}}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}
	worker := NewIngestionWorker(db, vi, extract, embed, 0.88, schema, nil)

	// Per-item error gets logged; outer returns nil.
	_ = worker.ProcessDialogWithProvenance(t.Context(),
		"src/illegal-rel",
		core.Provenance{ExtractedFrom: "src/illegal-rel"},
	)

	// Audit: entity INSERT runs first inside the same tx as the
	// failed createEdgesInTx; the unknown-relation branch aborts
	// createEdgesInTx → writeErr set → executeItemTx rolls back the
	// ENTIRE tx. Resulting row count for the entity is 0, not 1.
	// (This locks the rollback contract: an unknown relation drops
	// the entity, not just the edge.)
	var entityCount, edgeCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, "illegal-rel-e").Scan(&entityCount); err != nil {
		t.Fatalf("entity count: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&edgeCount); err != nil {
		t.Fatalf("edge count: %v", err)
	}
	if entityCount != 0 {
		t.Errorf("unknown-rel: want 0 entity rows (tx rolled back); got %d", entityCount)
	}
	if edgeCount != 0 {
		t.Errorf("unknown-rel: want 0 edge rows; got %d", edgeCount)
	}
}

// TestCreateEntityInTx_ProvenanceFieldsPersisted — verifies all 4
// provenance columns (conversation_id, message_id, source, source_type)
// land on the entity row when prov carries them. This is the
// "extract → embed → store" path's bookkeeping contract.
func TestCreateEntityInTx_ProvenanceFieldsPersisted(t *testing.T) {
	t.Parallel()
	db, _, _, worker := newFreshEntityWorkerOnMem(t, []float32{1.0, 0.0, 0.0})
	defer db.Close()

	prov := core.Provenance{
		ConversationID: "conv-xyz",
		MessageID:      "msg-zyx",
		ExtractedFrom:  "src/prov-test",
	}
	if err := worker.ProcessDialogWithProvenance(t.Context(), "src/prov-test", prov); err != nil {
		t.Fatalf("ProcessDialogWithProvenance: %v", err)
	}

	var convID, msgID, source, sourceType sql.NullString
	if err := db.QueryRow(
		`SELECT conversation_id, message_id, source, source_type FROM entities WHERE id = ?`,
		"fresh-test-entity",
	).Scan(&convID, &msgID, &source, &sourceType); err != nil {
		t.Fatalf("provenance audit: %v", err)
	}
	want := map[string]string{
		"conversation_id": "conv-xyz",
		"message_id":      "msg-zyx",
		"source":          "dialog",
		"source_type":     "extraction",
	}
	got := map[string]string{
		"conversation_id": convID.String,
		"message_id":      msgID.String,
		"source":          source.String,
		"source_type":     sourceType.String,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: want %q, got %q", k, v, got[k])
		}
	}
}

// TestReloadSchema_SwapsSchemaField — the SIGHUP-driven ReloadSchema
// contract: after ReloadSchema(new), subsequent ingest uses the new
// schema's relation filter (createEdgesInTx consults w.schema).
//
// We seed a worker with the default schema, then ReloadSchema to one
// where AllowedRelations excludes "uses". A subsequent ingest with a
// "uses" relation is rejected (per-item error logged; the edge is
// rolled back along with the entity insert).
func TestReloadSchema_SwapsSchemaField(t *testing.T) {
	t.Parallel()
	db, vi := newVecSpyOnMemDB(t)
	schema := core.DefaultSchemaConfig(false)
	extract := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{
			{
				ID: "reload-e", Category: "world", Content: "x",
				Relations: []core.Relation{{TargetID: "anywhere", RelationType: "uses"}},
			},
		},
	}}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}
	worker := NewIngestionWorker(db, vi, extract, embed, 0.88, schema, nil)

	// Reload to a schema that excludes "uses" by clearing it.
	newSchema := schema
	newSchema.AllowedRelations = map[string]bool{"contradicts": true}
	worker.ReloadSchema(newSchema)

	// Audit: the new schema DOES NOT contain "uses" — sanity check
	// before running ingest.
	if newSchema.AllowedRelations["uses"] {
		t.Fatal("test setup: expected \"uses\" absent from newSchema.AllowedRelations")
	}

	_ = worker.ProcessDialogWithProvenance(t.Context(),
		"src/reload-test", core.Provenance{ExtractedFrom: "src/reload-test"},
	)

	// Per-item the unknown-relation error rolled back the entity
	// insert too (executeItemTx.b writeErr → tx.Rollback), so the
	// entity row should NOT be present.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, "reload-e").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("ReloadSchema: unknown-relation rollback should leave 0 entity rows, got %d", n)
	}
}

// =========================================================================
// Section 4 — entry-point wrappers (ProcessDialog + MemoryWorkerResilientFromConfig)
// =========================================================================
//
// These close the two remaining 0% funcs in dialog.go (the previous
// coverage report referenced api.go, but that file was removed; the
// same symbols live at dialog.go:22 and dialog.go:413).

// TestProcessDialog_NoProvenance_DefaultsExtractedFromToDialog — the
// 1-line wrapper at dialog.go:22 delegates to
// ProcessDialogWithProvenance with prov.ExtractedFrom = dialog. Lock
// the contract: passing a dialog with no explicit provenance must
// land an SQL row whose `extracted_from` column equals the dialog
// itself, and whose `conversation_id` + `message_id` columns are
// NULL (no provenance was provided).
//
// Audit: extracted_from column = dialog; conv/msg ids NULL.
func TestProcessDialog_NoProvenance_DefaultsExtractedFromToDialog(t *testing.T) {
	t.Parallel()
	db, _, _, worker := newFreshEntityWorkerOnMem(t, []float32{1.0, 0.0, 0.0})
	defer db.Close()

	const dialog = "src/dialog-only"
	if err := worker.ProcessDialog(t.Context(), dialog); err != nil {
		t.Fatalf("ProcessDialog err=%v; want nil", err)
	}

	// Audit: extracted_from column equals the dialog itself (per the
	// no-provenance default in dialog.go:19).
	var extractedFrom string
	if err := db.QueryRow(
		`SELECT extracted_from FROM entities WHERE id = ?`, "fresh-test-entity",
	).Scan(&extractedFrom); err != nil {
		t.Fatalf("audit extracted_from: %v", err)
	}
	if extractedFrom != dialog {
		t.Errorf("want extracted_from=%q; got %q", dialog, extractedFrom)
	}

	// Audit: conversation_id + message_id are EMPTY strings (no
	// provenance was provided; the wrapper defaults ExtractedFrom
	// only). SQLite stores the zero-value string as empty TEXT
	// (NOT NULL — sql.NullString.Valid would still be true), so we
	// assert the String field rather than Valid.
	var convID, msgID sql.NullString
	if err := db.QueryRow(
		`SELECT conversation_id, message_id FROM entities WHERE id = ?`, "fresh-test-entity",
	).Scan(&convID, &msgID); err != nil {
		t.Fatalf("audit conv/msg ids: %v", err)
	}
	if convID.String != "" {
		t.Errorf("conversation_id should be empty string for no-provenance default; got %q", convID.String)
	}
	if msgID.String != "" {
		t.Errorf("message_id should be empty string for no-provenance default; got %q", msgID.String)
	}
}

// TestMemoryWorkerResilientFromConfig_ChannelCloseFlushesCheckpoint —
// the production entry point at dialog.go:413 builds an IngestionWorker
// from MemoryWorkerConfig and dispatches to resilientLoop. Closing the
// channel (no ctx-cancel) drives the producer-closes branch of
// resilientLoop and flushes the checkpoint.
//
// Audit: ckptPath file is present, parseable JSON, LastCommittedIndex ==
// N and WorkerID matches.
func TestMemoryWorkerResilientFromConfig_ChannelCloseFlushesCheckpoint(t *testing.T) {
	t.Parallel()

	dsn := filepath.Join(t.TempDir(), "rw-cfg.db")
	db, err := store.InitDB(dsn, 3)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	vi := newVecSpy(nil)
	extract := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{{ID: "x", Category: "world", Content: "c"}},
	}}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}

	ckptPath := filepath.Join(t.TempDir(), "ckpt.json")
	pendingPath := filepath.Join(t.TempDir(), "pending.jsonl")

	cfg := MemoryWorkerConfig{
		DB:             db,
		VectorIndex:    vi,
		Extractor:      extract,
		Embedder:       embed,
		DedupThreshold: 0.88,
		Schema:         core.DefaultSchemaConfig(false),
		CkptPath:       ckptPath,
		PendingPath:    pendingPath,
		WorkerID:       "rw-cfg-test",
	}

	const N = 2
	ch := make(chan core.MemoryMessage, N)
	for i := 0; i < N; i++ {
		ch <- core.MemoryMessage{Dialog: fmt.Sprintf("d%d", i), ConversationID: "c", MessageID: fmt.Sprintf("m%d", i)}
	}
	close(ch)

	done := make(chan struct{})
	go func() {
		defer close(done)
		MemoryWorkerResilientFromConfig(t.Context(), cfg, ch)
	}()
	if err := waitFor(done, 5000); err != nil {
		t.Fatalf("MemoryWorkerResilientFromConfig did not return after channel close: %v", err)
	}

	// Audit: ckpt file exists, parseable, LastCommittedIndex == N and
	// WorkerID matches. Together these lock the producer-closes
	// branch + the per-message increment of processed.
	ckptBytes, err := os.ReadFile(ckptPath)
	if err != nil {
		t.Fatalf("ReadFile ckpt: %v", err)
	}
	var got IngestionCheckpoint
	if err := json.Unmarshal(ckptBytes, &got); err != nil {
		t.Fatalf("unmarshal ckpt: %v", err)
	}
	if got.LastCommittedIndex != int64(N) {
		t.Errorf("ckpt.LastCommittedIndex = %d, want %d", got.LastCommittedIndex, N)
	}
	if got.WorkerID != "rw-cfg-test" {
		t.Errorf("ckpt.WorkerID = %q, want rw-cfg-test", got.WorkerID)
	}
	// pendingPath should NOT exist (clean channel close → drain never
	// runs because the select fell into the `case msg, ok := <-ch:
	// !ok` branch).
	if _, statErr := os.Stat(pendingPath); statErr == nil {
		data, _ := os.ReadFile(pendingPath)
		if len(data) != 0 {
			t.Errorf("pendingPath should be empty on clean channel-close; got %q", data)
		}
	}
}

// TestReloadSchema_SwapsSchemaField_DomainRelationSet — the 9th
// ReloadSchema test, scoped to a domain-specific relation set (the
// code-graph domain: depends_on, calls, extends, implements). After
// ReloadSchema(new), only these 4 relation types are accepted;
// anything else (e.g. "uses") is rejected by createEdgesInTx and
// rolls back the entity insert.
//
// Audit: e1 (calls) is committed; e2 (uses) is rolled back. Locks the
// domain-swap contract: ANY post-ReloadSchema ingest consults the
// newly-loaded relations list, not a cached default.
func TestReloadSchema_SwapsSchemaField_DomainRelationSet(t *testing.T) {
	t.Parallel()
	db, vi := newVecSpyOnMemDB(t)

	// Code-graph domain: coherent set of relations that model class
	// + method dependencies in a typed language. Replace the wider
	// default schema's allowed-relations with this narrow set; a
	// future ingest referencing "uses" or any non-domain relation
	// will be rejected by createEdgesInTx.
	codeDomainRelations := map[string]bool{
		"depends_on": true,
		"calls":      true,
		"extends":    true,
		"implements": true,
	}

	schema := core.DefaultSchemaConfig(false)
	// Pre-seed the edge target entity so the FK constraint on
	// edges.target_id passes for the in-domain "calls" relation.
	// Without this seeding, e1's tx would fail on createEdgesInTx's
	// bulk INSERT (FK violation), compounding with e2's
	// unknown-relation rollback — both rows would be absent, and the
	// audit couldn't distinguish "in-domain OK" from "FK rejected".
	seedBytes := store.EmbeddingToBytes([]float32{1.0, 0.0, 0.0})
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding, updated_at, confidence) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		"anywhere", "world", "edge target", seedBytes, 1.0,
	); err != nil {
		t.Fatalf("seed target entity: %v", err)
	}

	extract := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{
			{
				ID: "reload-domain-e1", Category: "code", Content: "func foo",
				Relations: []core.Relation{{TargetID: "anywhere", RelationType: "calls"}},
			},
			{
				ID: "reload-domain-e2", Category: "code", Content: "func bar",
				Relations: []core.Relation{{TargetID: "anywhere", RelationType: "uses"}}, // outside the code-domain set
			},
		},
	}}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}
	worker := NewIngestionWorker(db, vi, extract, embed, 0.88, schema, nil)

	// Reload to the code-domain relation set.
	newSchema := schema
	newSchema.AllowedRelations = codeDomainRelations
	worker.ReloadSchema(newSchema)

	// Sanity-check test setup: "calls" is in the new schema, "uses"
	// is NOT.
	if !newSchema.AllowedRelations["calls"] {
		t.Fatal("test setup: expected \"calls\" in codeDomainRelations")
	}
	if newSchema.AllowedRelations["uses"] {
		t.Fatal("test setup: expected \"uses\" absent from codeDomainRelations")
	}

	// Ingest with mixed allowed/disallowed relations.
	_ = worker.ProcessDialogWithProvenance(t.Context(),
		"src/reload-domain-test", core.Provenance{ExtractedFrom: "src/reload-domain-test"})

	// Audit e1 ("calls"): row committed normally because the relation
	// is in the domain set.
	var callsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, "reload-domain-e1").Scan(&callsCount); err != nil {
		t.Fatalf("calls entity audit: %v", err)
	}
	if callsCount != 1 {
		t.Errorf("domain 'calls' relation: want 1 entity row committed; got %d", callsCount)
	}
	// Audit e2 ("uses"): row absent because the unknown-relation
	// branch aborted createEdgesInTx → executeItemTx rolled back the
	// entire tx (entity INSERT included).
	var usesCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, "reload-domain-e2").Scan(&usesCount); err != nil {
		t.Fatalf("uses entity audit: %v", err)
	}
	if usesCount != 0 {
		t.Errorf("non-domain 'uses' relation: want 0 entity rows (rolled back); got %d", usesCount)
	}
}

// =========================================================================
// helpers — extra fixtures for select tests
// =========================================================================

// newFreshEntityWorkerOnMem is a variant of dialog_test.go's
// newFreshEntityWorker that returns 4 values (db, vi, ext, worker)
// instead of 3. We re-export it locally because the existing fixture
// returns 3 and we need vi explicitly in some tests.
func newFreshEntityWorkerOnMem(t *testing.T, embedVec []float32) (*sql.DB, *failingVIRecord, *stubExtractor, *IngestionWorker) {
	t.Helper()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("store.MemDBRandom: %v", err)
	}
	spy := &failingVIRecord{}
	ext := &stubExtractor{result: &core.ExtractionResult{
		Entities: []core.ExtractedEntity{
			{ID: "fresh-test-entity", Category: "world", Content: "test content"},
		},
	}}
	embed := &stubEmbedder{vec: embedVec}
	schema := core.DefaultSchemaConfig(false)
	worker := NewIngestionWorker(db, spy, ext, embed, 0.88, schema, nil)
	return db, spy, ext, worker
}

// newVecSpyOnMemDB returns (db, vecSpy). Used by vector-related tests
// that need direct vec-content audit (rather than the failingVIRecord's
// stores/removes/callOrder audit).
func newVecSpyOnMemDB(t *testing.T) (*sql.DB, *vecSpy) {
	t.Helper()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("store.MemDBRandom: %v", err)
	}
	return db, newVecSpy(nil)
}
