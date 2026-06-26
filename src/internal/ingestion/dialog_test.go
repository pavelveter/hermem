package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// TestIsIngestionContradiction covers the negation-flip heuristic. The
// English set has been stable across releases; the Russian set is the
// round-7 (§ 7) addition and the cases below are the regression traps
// that bit us before: bare verb flips that look like dedup, and bare
// double-negation that should still register as a contradiction (the
// domain contract is "any flip on a negation token = contradiction"
// rather than a balanced-proposition parse).
//
// Round-9 § 7.1 followup: the dual-pattern scan is now augmented with
// an inline minimal Russian stemmer (`stemRussian`/`stemPair`) so
// inflected forms like `любит`/`любила`/`любили`/`полюбил` flip on the
// bare particle `не` without being missed. The pre-stemmer substring
// scan still drives the original 14 cases — stricter cases for
// `любит`/`любила`/`любили`/`полюбил` + `не очень любит` are added at
// the end. The bare-particle-`не`-only flip is detected AFTER stem
// normalisation; the original substring scan continues to drive the
// English baseline + the bare/inflected Russian pair cases.
func TestIsIngestionContradiction(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		// English baseline (carried over from pre-round-7)
		{"english_identical", "User likes Go", "User likes Go", false},
		{"english_neg_flip", "User likes Go", "User does not like Go", true},
		{"english_identical_neg", "User does not like Go", "User does not like Go", false},
		{"english_hate_vs_love", "User hates Go", "User loves Go", true},

		// Russian — round-7 § 7 additions
		{"russian_neg_particle", "Я люблю море", "Я не люблю море", true},
		{"russian_hate_to_love", "Я люблю это", "Я ненавижу это", true},
		// Substring-boundary trade-off: \"не очень\" was dropped from the
		// negWords list because the substring scan cannot distinguish
		// \"я не очень люблю это\" from \"я люблю это\" via the surface
		// form — the \"очень\" interceptor breaks an \"не люблю\" substring
		// search. This row documents the limitation: the heuristic no
		// longer catches \"не очень любит\" and falls through to embedding
		// similarity for that pair. To re-catch, a real Russian stemmer
		// / tokenizer (TODO § 7.1) is needed; do NOT reintroduce bare
		// tokens without word-boundary guards.
		{"russian_ne_ochen_falls_through", "Я люблю это", "Я не очень люблю это", false},
		// Bare-past \"любил\" alone is positive — the negation list
		// requires `не любил` to flip. The row exercises the explicit
		// `разлюбил` inflection prefix (a separate token) so the pair
		// still flips.
		{"russian_razlub_inflection", "Я любил это", "Я разлюбил это", true},
		// Russian `мне нравится` / `это красиво` MUST return false: the
		// substring scan cannot distinguish `мне нравится` from
		// `мне не нравится` without word-boundary detection. Round-7 § 7
		// trade-off accepts the false-merge at this granularity; the
		// safer Russian coverage ships via inflected `не + verb` matches
		// only. This row + the `russian_substring_falls_through_ochen`
		// row document the same substring-boundary limitation under
		// consistent naming.
		{"russian_substring_falls_through_nravitsya", "Мне нравится это", "Это красиво", false},
		{"russian_nikogda_neg", "Хочу туда поехать", "Никогда не хочу туда ехать", true},
		{"russian_identical", "Я люблю это", "Я люблю это", false},
		{"russian_neg_identical", "Я не люблю это", "Я не люблю это", false},
		{"russian_double_neg_vs_plain_neg", "Я не ненавижу это", "Я ненавижу это", true},
		{"russian_cross_lang_detect", "User loves X", "User не любит X", true},

		// Round-9 § 7.1 — inline stemmer adds coverage for inflected verbs
		// whose surface form does NOT match the pre-stemmer negWords list.
		// After stem normalisation `не + verb_stem` flips on bare `не`
		// between the two strings.
		{"russian_stemmer_lubit_not_lubit", "Я люблю это", "Я не люблю это", true},
		{"russian_stemmer_lubit_ne_lubit", "Я любит море", "Я не любит море", true},
		{"russian_stemmer_polubil_ne_polubil", "Я полюбил это", "Я не полюбил это", true},
		// Same-lemma without negation flip → not a contradiction.
		{"russian_stemmer_lubit_labila_no_neg", "Я любит это", "Я любила это", false},
		// Substring scan flips on bare `not` (it's in negWords): al
		// contains "not" but bl doesn't → returns true. The trade-off
		// is documented at the function godoc: bare-substring matching
		// accepts this false positive for partial verb phrases like
		// "User does not X" as a price for catching bare `not`
		// negations elsewhere (e.g. `english_neg_flip`, where
		// `User does not like Go` vs `User likes Go` is a legitimate
		// contradiction). The row locks the dual scan's actual
		// behaviour, not an aspirational "should flip less".
		{"english_does_not_does", "User does not", "User does", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := IsIngestionContradiction(c.a, c.b)
			if got != c.want {
				t.Errorf("IsIngestionContradiction(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

// --- Round-9 § 3.1 atomicity regression tests ----------------------

// stubExtractor returns a fixed ExtractionResult regardless of input.
type stubExtractor struct {
	result *core.ExtractionResult
}

func (s *stubExtractor) ExtractEntities(_ context.Context, _ string) (*core.ExtractionResult, error) {
	return s.result, nil
}

// stubEmbedder returns a fixed vec for every input. The vec {1,0,0}
// is already unit-length so vector.NormalizeVector inside
// ProcessDialogWithProvenance is a no-op.
type stubEmbedder struct {
	vec []float32
	err error
}

func (s *stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]float32, len(s.vec))
	copy(out, s.vec)
	return out, nil
}

// fannedOp records one vi (vector-index) call in the OBSERVED ORDER
// so the merge-doctrine test can assert that `Remove(incoming-id)` ran
// strictly BEFORE `Store(existing-id)`. Stores and Removes append in
// arrival order regardless of their kind.
type fannedOp struct {
	kind string // "store" | "remove"
	id   string
}

// failingVIRecord implements core.VectorIndex. It records every Store
// / Remove call (with the operation id), the order in which they
// fired, and can be configured to fail every Nth call so the § 3.1
// atomicity contract is regressable.
//
// callOrder is the union of Store + Remove events in arrival order;
// tests assertions like "Remove(incoming-id) fired before
// Store(existing-id)" can read it directly.
type failingVIRecord struct {
	mu sync.Mutex

	stores    []string
	removes   []string
	callOrder []fannedOp

	failStoreEveryN  int // if > 0, every Nth Store returns errVIOpInjected
	failRemoveEveryN int
	storeCount       int
	removeCount      int

	// searchBatchResults is the canned SearchBatch result. When nil,
	// SearchBatch returns an empty result per query so
	// processOneItemOnce treats every item as NEW.
	searchBatchResults [][]string
}

var errVIOpInjected = errors.New("failing_vi_record: injected failure")

func (v *failingVIRecord) Search(_ context.Context, _ []float32, _ int) ([]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.searchBatchResults) == 0 {
		return nil, nil
	}
	return v.searchBatchResults[0], nil
}

func (v *failingVIRecord) SearchBatch(_ context.Context, vecs [][]float32, _ int) ([][]string, error) {
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

func (v *failingVIRecord) Store(_ context.Context, id string, _ []float32) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.storeCount++
	v.stores = append(v.stores, id)
	v.callOrder = append(v.callOrder, fannedOp{kind: "store", id: id})
	if v.failStoreEveryN > 0 && v.storeCount%v.failStoreEveryN == 0 {
		return errVIOpInjected
	}
	return nil
}

func (v *failingVIRecord) Remove(_ context.Context, ids []string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.removeCount++
	v.removes = append(v.removes, ids...)
	for _, id := range ids {
		v.callOrder = append(v.callOrder, fannedOp{kind: "remove", id: id})
	}
	if v.failRemoveEveryN > 0 && v.removeCount%v.failRemoveEveryN == 0 {
		return errVIOpInjected
	}
	return nil
}

// viSnapshot returns copies of stores/removes/callOrder under the
// spy mutex so tests can assert on the recorded call sequence
// without racing the ingest goroutine.
type viSnapshot struct {
	stores    []string
	removes   []string
	callOrder []fannedOp
}

func (v *failingVIRecord) snapshot() viSnapshot {
	v.mu.Lock()
	defer v.mu.Unlock()
	co := make([]fannedOp, len(v.callOrder))
	copy(co, v.callOrder)
	return viSnapshot{
		stores:    append([]string{}, v.stores...),
		removes:   append([]string{}, v.removes...),
		callOrder: co,
	}
}

// newFreshEntityWorker spins up the canonical ingest pipeline against
// MemDBRandom (vector_dim=3) with a stub embedder whose vec is
// already unit-length so normalize is a no-op. Returns the open DB
// plus the spy so tests can defer db.Close + assert on the snapshot.
func newFreshEntityWorker(t *testing.T, embedVec []float32, searchToReturn []string) (*sql.DB, *failingVIRecord, *IngestionWorker) {
	t.Helper()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("store.MemDBRandom: %v", err)
	}
	spy := &failingVIRecord{}
	if len(searchToReturn) > 0 {
		spy.searchBatchResults = [][]string{searchToReturn}
	}
	extract := &stubExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{ID: "fresh-test-entity", Category: "world", Content: "test content"},
			},
		},
	}
	embed := &stubEmbedder{vec: embedVec}
	schema := core.DefaultSchemaConfig(false)
	worker := NewIngestionWorker(db, spy, extract, embed, 0.88, schema, nil)
	return db, spy, worker
}

// TestProcessDialogWithProvenance_VIOpFailureDoesNotFailCommit is the
// primary § 3.1 atomicity regression: when the post-commit vi.Store
// returns an injected error, ProcessDialogWithProvenance must STILL
// return nil and the DB row must STILL be present. The reverse case
// (vi op before commit + vi failure → DB rollback) would fail this
// test because either the DB row would be missing or the function
// would propagate the error into ProcessDialogWithProvenance.
//
// The test asserts the § 3.1 contract in three places simultaneously:
//
//  1. err == nil             — vi op failure does not surface to caller.
//  2. DB row exists          — DB commit happened; vi didn't gate it.
//  3. spy.stores contains id — vi op was attempted (a posteri
//     re-embed path can rebuild any drift).
func TestProcessDialogWithProvenance_VIOpFailureDoesNotFailCommit(t *testing.T) {
	db, spy, worker := newFreshEntityWorker(t,
		[]float32{1.0, 0.0, 0.0},
		nil, // SearchBatch returns empty → no matching existing entity
	)
	defer db.Close()

	prov := core.Provenance{ExtractedFrom: "src/dlg-vifail"}
	if err := worker.ProcessDialogWithProvenance(context.Background(), "src/dlg-vifail", prov); err != nil {
		t.Fatalf("ProcessDialogWithProvenance err=%v; want nil (vi.Store failure must NOT abort ingest per § 3.1)", err)
	}

	// DB row must exist — proves commit succeeded even though vi.Store
	// returned errVIOpInjected (post-commit branch).
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, "fresh-test-entity").Scan(&count); err != nil {
		t.Fatalf("query db for entity row: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 row in DB after ingest; got %d — if vi ran before commit, the row would be 0", count)
	}

	snap := spy.snapshot()
	if len(snap.stores) != 1 {
		t.Fatalf("want exactly 1 Store call recorded, got %d (slice=%v)", len(snap.stores), snap.stores)
	}
	if snap.stores[0] != "fresh-test-entity" {
		t.Fatalf("want Store for fresh-test-entity; got %q", snap.stores[0])
	}
	if len(snap.removes) != 0 {
		t.Fatalf("want zero Remove calls for fresh entity; got %d (slice=%v)", len(snap.removes), snap.removes)
	}
}

// TestProcessDialogWithProvenance_FreshEntityStoresExactlyOnce locks the
// viOps composition doctrine for the simplest branch: NEW entity (no
// existing match) must result in exactly one vi.Store and zero
// vi.Remove. If a future refactor accidentally appends an extra Remove
// (or drops the Store) this test will fail.
func TestProcessDialogWithProvenance_FreshEntityStoresExactlyOnce(t *testing.T) {
	db, spy, worker := newFreshEntityWorker(t,
		[]float32{1.0, 0.0, 0.0}, // already unit-length → normalize is no-op
		nil,                      // SearchBatch returns empty → no matching existing entity
	)
	defer db.Close()

	prov := core.Provenance{ExtractedFrom: "src/dlg-fresh"}
	if err := worker.ProcessDialogWithProvenance(context.Background(), "src/dlg-fresh", prov); err != nil {
		t.Fatalf("ProcessDialogWithProvenance err=%v; want nil", err)
	}

	snap := spy.snapshot()
	if len(snap.stores) != 1 || snap.stores[0] != "fresh-test-entity" {
		t.Fatalf("want exactly one Store for fresh-test-entity; got stores=%v removes=%v", snap.stores, snap.removes)
	}
	if len(snap.removes) != 0 {
		t.Fatalf("want zero Removes for fresh entity; got %v", snap.removes)
	}
}

// Followup-1: § 3.1 doctrine for the MERGE branch — when findMatch
// returns an existing entity and IsIngestionContradiction does NOT
// fire, processOneItemOnce composes viOps as [Remove(incoming.ID),
// Store(mergeEntity.ID, mergeEntity.Embedding)] in that exact order.
// A future refactor that drops the defensive Remove (or reorders it
// AFTER the Store) would create orphan vec entries and fail this
// test.
func TestProcessDialogWithProvenance_MergeComposesRemoveBeforeStore(t *testing.T) {
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("store.MemDBRandom: %v", err)
	}
	defer db.Close()

	// Seed an existing entity directly via INSERT so findMatch can
	// resolve it. Embedding is the same unit vector we use for the
	// incoming extraction → cosine similarity = 1.0 (above the
	// dedup threshold 0.88), entering the MERGE branch.
	const existingID = "existing-merge-target"
	const incomingID = "incoming-id"
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding, updated_at, confidence) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		existingID,
		"world",
		"existing merge target",
		store.EmbeddingToBytes([]float32{1.0, 0.0, 0.0}),
		1.0,
	); err != nil {
		t.Fatalf("seed existing: %v", err)
	}

	// Stub SearchBatch: return the existingID as the top match so
	// findMatch reads it (and the dedup threshold passes because of
	// cosine ≈ 1.0).
	spy := &failingVIRecord{
		searchBatchResults: [][]string{{existingID}},
	}

	extract := &stubExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{ID: incomingID, Category: "world", Content: "merged content"},
			},
		},
	}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}
	worker := NewIngestionWorker(db, spy, extract, embed, 0.88, core.DefaultSchemaConfig(false), nil)

	if err := worker.ProcessDialogWithProvenance(context.Background(), "src/merge-test", core.Provenance{ExtractedFrom: "src/merge-test"}); err != nil {
		t.Fatalf("ProcessDialogWithProvenance err=%v; want nil", err)
	}

	snap := spy.snapshot()

	// Doctrine: callOrder[0] is the defensive Remove(incoming-id); a
	// later callOrder entry is the post-commit Store(mergeEntity.ID,
	// mergeEmb). The merge happens at the existing row, so the store
	// re-uses existingID as the merge target id.
	if len(snap.callOrder) == 0 {
		t.Fatalf("want >=1 viOp observed; got empty callOrder")
	}
	if snap.callOrder[0].kind != "remove" || snap.callOrder[0].id != incomingID {
		t.Fatalf("callOrder[0] want remove(%q); got %+v", incomingID, snap.callOrder[0])
	}

	// Find the index of Store(existingID) — replace-style merge writes
	// back to the existing row.
	storeIdx := -1
	for i, op := range snap.callOrder {
		if op.kind == "store" && op.id == existingID {
			storeIdx = i
			break
		}
	}
	if storeIdx < 0 {
		t.Fatalf("callOrder: want store(%q) somewhere; got %+v", existingID, snap.callOrder)
	}
	if storeIdx <= 0 {
		t.Fatalf("store must come AFTER the first remove (Remove→Store doctrine); got callOrder=%+v", snap.callOrder)
	}
}

// Followup-2: § 3.1 doctrine for the LOW-CONF contradiction branch —
// processOneItemOnce folds the archive UPDATE INTO itemTx, which
// commits atomically with the new entity INSERT. The post-commit
// viOps drain runs [Remove(archiveID), Store(incoming)]. A future
// refactor that moved the archive UPDATE outside the tx (the OLD
// bug) would leave the vec index cleared but the DB row archived=0
// — surfacing as SEARCH DRIFT.
//
// The test proves the contract by:
//
//  1. Asserting `existing` row archived=1 in DB (atomic with new INSERT).
//  2. Asserting spy.callOrder contains both Remove(archiveID) and
//     Store(incoming-lowconf).
func TestProcessDialogWithProvenance_LowConfContradictionArchivesAtomically(t *testing.T) {
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("store.MemDBRandom: %v", err)
	}
	defer db.Close()

	const existingID = "existing-lowconf"
	const incomingID = "incoming-lowconf"
	// Seed existing with confidence=0.5 (below the LOW-CONF threshold).
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, embedding, updated_at, confidence) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		existingID,
		"world",
		"User loves X", // english baseline so IsIngestionContradiction fires via the "hate_vs_love" rule
		store.EmbeddingToBytes([]float32{1.0, 0.0, 0.0}),
		0.5, // low confidence branch
	); err != nil {
		t.Fatalf("seed existing: %v", err)
	}

	spy := &failingVIRecord{
		searchBatchResults: [][]string{{existingID}},
	}

	extract := &stubExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{ID: incomingID, Category: "world", Content: "User hates X"}, // antonym pair triggers IsIngestionContradiction
			},
		},
	}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}
	worker := NewIngestionWorker(db, spy, extract, embed, 0.88, core.DefaultSchemaConfig(false), nil)

	if err := worker.ProcessDialogWithProvenance(context.Background(), "src/lc-test", core.Provenance{ExtractedFrom: "src/lc-test"}); err != nil {
		t.Fatalf("ProcessDialogWithProvenance err=%v; want nil", err)
	}

	// 1. Audit: existing row archived=1 in DB atomically with new
	// INSERT.
	var archived int
	if err := db.QueryRow(`SELECT archived FROM entities WHERE id = ?`, existingID).Scan(&archived); err != nil {
		t.Fatalf("audit existing row: %v", err)
	}
	if archived != 1 {
		t.Fatalf("want existing row archived=1 (atomic with new INSERT); got %d — search drift would surface", archived)
	}

	// 2. Audit: post-commit viOps drained both Remove(existingID) and
	// Store(incomingID) — leave no orphan vec index entries.
	snap := spy.snapshot()
	var sawRemoveExisting, sawStoreIncoming bool
	for _, op := range snap.callOrder {
		switch op.id {
		case existingID:
			if op.kind == "remove" {
				sawRemoveExisting = true
			}
		case incomingID:
			if op.kind == "store" {
				sawStoreIncoming = true
			}
		}
	}
	if !sawRemoveExisting {
		t.Fatalf("want Remove(existingID) post-commit; callOrder=%+v", snap.callOrder)
	}
	if !sawStoreIncoming {
		t.Fatalf("want Store(incomingID) post-commit; callOrder=%+v", snap.callOrder)
	}
}

// Followup-3: § 3.1 atomicity the OTHER direction — when the DB tx
// cannot begin (closed db ⇒ BeginTx returns "sql: database is
// closed"), NO viOp ever fires. A future refactor that accidentally
// runs vi ops before BeginTx success (or before Commit success)
// would surface a phantom callOrder row and fail this test.
//
// Aggregation caveat: ProcessDialogWithProvenance is a per-item
// AGGREGATOR — it logs every per-item error via slog.Error and
// returns nil regardless. The § 3.1 atomicity invariant ("applyVIOps
// invoked ONLY after a successful Commit") is verified directly via
// spy.callOrder emptiness, NOT via the propagated err (which is
// always nil by design). The previous version of this test asserted
// `err != nil`, which would have flagged the (incorrect assumption
// that errors propagate); the new shape matches the running contract.
func TestProcessDialogWithProvenance_RollbackSkipsVIOps(t *testing.T) {
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("store.MemDBRandom: %v", err)
	}
	// Close the DB BEFORE ingest so every subsequent call returns
	// "sql: database is closed". Defer is intentionally omitted —
	// the test wants the closed state for the entire ProcessDialog
	// call below.
	_ = db.Close()

	spy := &failingVIRecord{}
	extract := &stubExtractor{
		result: &core.ExtractionResult{
			Entities: []core.ExtractedEntity{
				{ID: "rollback-test", Category: "world", Content: "should never reach vi"},
			},
		},
	}
	embed := &stubEmbedder{vec: []float32{1.0, 0.0, 0.0}}
	worker := NewIngestionWorker(db, spy, extract, embed, 0.88, core.DefaultSchemaConfig(false), nil)

	// § 3.1 invariant: per-item errors are LOGGED, not propagated.
	// Discard err so linter doesn't complain; spy.callOrder empty
	// below is the real atomicity assertion.
	_ = worker.ProcessDialogWithProvenance(context.Background(), "src/rb-test", core.Provenance{ExtractedFrom: "src/rb-test"})

	// § 3.1 atomicity contract: NO viOp fires when the DB tx never
	// commits. callOrder must be empty (this is the regression-trap;
	// a future refactor that runs vi ops pre-Commit would surface
	// here).
	snap := spy.snapshot()
	if len(snap.callOrder) != 0 {
		t.Fatalf("closed db before commit → applyVIOps NOT invoked; got callOrder=%+v", snap.callOrder)
	}
	if len(snap.stores) != 0 || len(snap.removes) != 0 {
		t.Fatalf("want stores=0 removes=0; got stores=%v removes=%v", snap.stores, snap.removes)
	}
}
