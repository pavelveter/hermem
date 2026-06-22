package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var ictx = context.Background()

// stubExtractor is a stub LLMExtractor that returns a fixed response /
// error without touching the network. Place it in the test file so it
// stays out of production builds.
type stubExtractor struct {
	resp *ExtractionResult
	err  error
}

func (s *stubExtractor) ExtractEntities(_ context.Context, _ string) (*ExtractionResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

// stubEmbedder is a deterministic Embedder stub. The vecs map lets the
// test pre-supply immutable vectors; default fallback is a fixed
// length-modulo vector so cosine is reproducible across runs.
type stubEmbedder struct {
	vecs  map[string][]float32
	err   error
	calls int
}

func (e *stubEmbedder) Embed(_ context.Context, content string) ([]float32, error) {
	e.calls++
	if e.err != nil {
		return nil, e.err
	}
	if v, ok := e.vecs[content]; ok {
		return v, nil
	}
	return []float32{float32(len(content)), float32(len(content) % 7), float32(len(content) % 5)}, nil
}

func TestProcessDialogHappyPath(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	ext := &stubExtractor{resp: &ExtractionResult{Entities: []ExtractedEntity{
		{ID: "e1", Category: "world", Content: "Paris is capital of France"},
		{ID: "e2", Category: "world", Content: "Berlin is capital of Germany"},
	}}}
	emb := &stubEmbedder{}
	w := NewIngestionWorker(db, vi, ext, emb, 0.99) // high threshold → no dedup
	if err := w.ProcessDialog(ictx, "user dialog text"); err != nil {
		t.Fatalf("ProcessDialog: %v", err)
	}
	rows, err := db.Query(`SELECT id FROM entities ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	if len(ids) != 2 {
		t.Errorf("entities in DB = %d (%v), want 2", len(ids), ids)
	}
}

// TestProcessDialogMergesNearDuplicate exercises the dedup path: an
// incoming entity with cosine >= threshold against an existing one
// must merge into the existing row (content not duplicated), leaving
// total row count at 1.
func TestProcessDialogMergesNearDuplicate(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	existingEmbed := []float32{0.4, 0.6, 0.8}
	if err := StoreEntityWithEmbedding(db, vi, Entity{
		ID: "old-1", Category: "world", Content: "Existing fact", Embedding: existingEmbed,
	}); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	ext := &stubExtractor{resp: &ExtractionResult{Entities: []ExtractedEntity{
		{ID: "new-1", Category: "world", Content: "Existing fact"},
	}}}
	emb := &stubEmbedder{vecs: map[string][]float32{
		"Existing fact": existingEmbed,
	}}
	w := NewIngestionWorker(db, vi, ext, emb, 0.99)
	if err := w.ProcessDialog(ictx, "d"); err != nil {
		t.Fatalf("ProcessDialog: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("row count after merge = %d, want 1", n)
	}
}

// TestProcessDialogEmbedderError ensures embedder failures propagate
// (don't get silently swallowed) so the LLM output isn't silently
func TestProcessDialogEmbedderError(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	embErr := errors.New("embedder down")
	ext := &stubExtractor{resp: &ExtractionResult{Entities: []ExtractedEntity{
		{ID: "x", Category: "world", Content: "x"},
	}}}
	emb := &stubEmbedder{err: embErr}
	w := NewIngestionWorker(db, vi, ext, emb, 0.99)
	err := w.ProcessDialog(ictx, "d")
	if err != nil {
		t.Fatalf("expected no error (entity errors are non-fatal), got %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, "x").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("entity was stored despite embedder error, want 0 rows")
	}
}

func TestProcessDialogExtractorError(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	extErr := errors.New("ollama offline")
	ext := &stubExtractor{err: extErr}
	emb := &stubEmbedder{}
	w := NewIngestionWorker(db, vi, ext, emb, 0.99)
	err := w.ProcessDialog(ictx, "d")
	if err == nil {
		t.Fatal("expected extractor error, got nil")
	}
	if !strings.Contains(err.Error(), "ollama offline") {
		t.Errorf("error chain lost extractor cause: got %v", err)
	}
}

// TestMemoryWorkerDrainsChannel confirms MemoryWorker completes when
// the channel is closed and writes the corresponding entity to the DB.
// This is a foreground synchronous flow: send 1 msg, close, wait, query.
func TestMemoryWorkerDrainsChannel(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	ext := &stubExtractor{resp: &ExtractionResult{Entities: []ExtractedEntity{
		{ID: "m1", Category: "world", Content: "MemoryWorkerDrainsChannel fact"},
	}}}
	emb := &stubEmbedder{}
	ch := make(chan MemoryMessage, 4)
	ch <- MemoryMessage{Dialog: "hello"}
	close(ch)

	MemoryWorker(ictx, db, vi, ext, emb, 0.99, ch)

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, "m1").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("MemoryWorker produced %d rows for id=m1, want 1", n)
	}
}

// TestMemoryWorkerExtractorErrorHaltsAllIngest verifies that when
// every message hits an extractor error, the worker logs each one
// (via slog.Error) but writes zero entities to the DB. embedder.calls
// stays at 0 because processEntity calls the embedder AFTER the
// extractor, so a failing extractor short-circuits before
// embedding is invoked.
func TestMemoryWorkerExtractorErrorHaltsAllIngest(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	ext := &stubExtractor{err: errors.New("first boom")}
	emb := &stubEmbedder{}
	ch := make(chan MemoryMessage, 2)
	ch <- MemoryMessage{Dialog: "first"}
	ch <- MemoryMessage{Dialog: "second"}
	close(ch)

	MemoryWorker(ictx, db, vi, ext, emb, 0.99, ch)

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("entity count after always-erroring worker = %d, want 0", n)
	}
	if emb.calls != 0 {
		t.Errorf("embedder called %d times despite always-erroring extractor, want 0", emb.calls)
	}
}

// statefulExtractor fails on its first ExtractEntities call and
// returns the supplied resp on subsequent calls. Used in the
// "recovers after single error" test.
type statefulExtractor struct {
	resp    *ExtractionResult
	err     error
	failOn  int
	callNum int
}

func (s *statefulExtractor) ExtractEntities(_ context.Context, _ string) (*ExtractionResult, error) {
	s.callNum++
	if s.callNum == s.failOn {
		return nil, s.err
	}
	return s.resp, nil
}

// TestMemoryWorkerContinuesAfterSingleError documents the loop's
// "log on error, keep going" contract: a transient extractor failure
// on the first message does NOT stop the second message from being
// processed. Verifies by sending a failing msg, a succeeding msg, and
// asserting exactly the second message's entity lands in the DB.
func TestMemoryWorkerContinuesAfterSingleError(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	ext := &statefulExtractor{
		resp: &ExtractionResult{Entities: []ExtractedEntity{
			{ID: "good-m2", Category: "world", Content: "good-m2 fact"},
		}},
		err:    errors.New("first-time-only boom"),
		failOn: 1,
	}
	emb := &stubEmbedder{}
	ch := make(chan MemoryMessage, 4)
	ch <- MemoryMessage{Dialog: "first (errors)"}
	ch <- MemoryMessage{Dialog: "second (succeeds)"}
	close(ch)

	MemoryWorker(ictx, db, vi, ext, emb, 0.99, ch)

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, "good-m2").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("MemoryWorker post-error recovered entity count = %d, want 1", n)
	}
	if ext.callNum != 2 {
		t.Errorf("extractor callNum = %d, want 2 (one failing, one succeeding)", ext.callNum)
	}
}

// TestProcessDialogAppendsDifferentContentOnMerge exercises the
// append-content branch of mergeEntities — the case where the
// incoming entity's content is NOT already contained in the
// existing entity's content. This is the route that calls the
// `mergedContent = existing + "; " + new` branch in mergeEntities,
// distinct from the equal-content path covered by
// TestProcessDialogMergesNearDuplicate.
func TestProcessDialogAppendsDifferentContentOnMerge(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	existingEmbed := []float32{0.4, 0.6, 0.8}
	if err := StoreEntityWithEmbedding(db, vi, Entity{
		ID: "old-2", Category: "world", Content: "apple", Embedding: existingEmbed,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ext := &stubExtractor{resp: &ExtractionResult{Entities: []ExtractedEntity{
		{ID: "new-2", Category: "world", Content: "banana"},
	}}}
	// Same vector as existing (cos = 1.0 ≥ 0.99, dedup triggers) but
	// DIFFERENT content (strings.Contains("apple", "banana") == false,
	// so the "\"; \"" separator branch runs). The merged content is
	// re-embedded by the worker; we pre-supply its deterministic vec.
	emb := &stubEmbedder{vecs: map[string][]float32{
		"banana":        existingEmbed,
		"apple; banana": existingEmbed,
	}}
	w := NewIngestionWorker(db, vi, ext, emb, 0.99)
	if err := w.ProcessDialog(ictx, "d"); err != nil {
		t.Fatalf("ProcessDialog: %v", err)
	}

	// Row count must remain 1 — dedup merged into existing, no new row.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("row count after content-append merge = %d, want 1", n)
	}
	// Merged content must include the "; " separator + both halves.
	var content string
	if err := db.QueryRow(`SELECT content FROM entities WHERE id = ?`, "old-2").Scan(&content); err != nil {
		t.Fatalf("read content: %v", err)
	}
	if content != "apple; banana" {
		t.Errorf("merged content = %q, want %q (append separator + both halves)", content, "apple; banana")
	}
}

// TestCreateEdgesBulkInsertFirstChunk covers the happy path for
// the bulk INSERT migration: ≤ ceil(DefaultSQLBatchSize/3) relations
// fit in a single multi-VALUES statement. We use 150 (well under
// the 166-row single-chunk ceiling) to leave clearance for any
// future bump in host parameters-per-edge.
func TestCreateEdgesBulkInsertFirstChunk(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	// FK enforcement is OFF in this codebase (no PRAGMA foreign_keys),
	// so the target rows don't need to exist as entities. We only
	// need entityID to be insertable as source_id, which has no
	// additional constraint beyond edges.source_id NOT NULL.
	w := NewIngestionWorker(db, vi, nil, nil, 0.99)

	const n = 150
	rels := make([]Relation, n)
	for i := range rels {
		rels[i] = Relation{
			TargetID:     "t-" + strconv.Itoa(i),
			RelationType: "related_to",
		}
	}
	if err := w.createEdges("src", rels); err != nil {
		t.Fatalf("createEdges %d: %v", n, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = ?`, "src").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != n {
		t.Errorf("edges count = %d, want %d", count, n)
	}
}

// TestCreateEdgesBulkInsertMultiChunk verifies bulk insertion
// properly partitions >chunkSize relations across multiple SQL
// statements; with edgesPerChunk=DefaultSQLBatchSize/3 (~166), a
// 700-row batch lands in ceil(700/166)=5 chunks. We only assert
// the total row count here; the chunk-count boundary is exercised
// implicitly by the row count being exact (off-by-one chunking
// would drop rows).
func TestCreateEdgesBulkInsertMultiChunk(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	w := NewIngestionWorker(db, vi, nil, nil, 0.99)

	const n = 700
	rels := make([]Relation, n)
	for i := range rels {
		rels[i] = Relation{
			TargetID:     "t-" + strconv.Itoa(i),
			RelationType: "related_to",
		}
	}
	if err := w.createEdges("src", rels); err != nil {
		t.Fatalf("createEdges %d: %v", n, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = ?`, "src").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != n {
		t.Errorf("edges count = %d, want %d (chunking may be wrong)", count, n)
	}
}

// TestCreateEdgesBulkEmptyInput verifies the no-op short-circuit:
// neither nil nor an empty relations slice should issue any SQL.
// Catches a regression where the function issued a useless
// "VALUES ()" INSERT with no rows.
func TestCreateEdgesBulkEmptyInput(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	w := NewIngestionWorker(db, vi, nil, nil, 0.99)

	if err := w.createEdges("src", nil); err != nil {
		t.Fatalf("createEdges(nil): %v", err)
	}
	if err := w.createEdges("src", []Relation{}); err != nil {
		t.Fatalf("createEdges([]): %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("edges after empty-input calls = %d, want 0", count)
	}
}

// TestStoreEntityConcurrentIndexOrderingFix exercises the #10 fix
// to StoreEntityWithEmbedding: with N goroutines storing distinct
// entities concurrently, every entity that survives in SQLite must
// also be retrievable from the vector index by its own embedding.
//
// File-backed DB: SQLite :memory: databases are per-connection
// (without ?cache=shared), and InitDB configures sql.DB's pool to
// 4 max open conns. 50 parallel goroutines rotate through those
// connections and any conn opened *after* schema setup never sees
// the tables. A real file via t.TempDir makes every connection
// point at the same on-disk schema; the dir auto-cleans.
//
// The fix bounds the post-fix transient: there's a brief window
// where the index has an entry before SQLite commits, so a
// concurrent Search could return an id that hasn't reached the DB
// yet — SearchByVector then returns fewer results than asked
// (the `len(results) < topK` signal). Acceptable trade-off vs
// a phantom index entry pointing at a row the DB doesn't hold.
//
// Run with -race: any data race in the index's reads/writes or in
// SQLite's connection sharing surfaces here.
func TestStoreEntityConcurrentIndexOrderingFix(t *testing.T) {
	tmpPath := filepath.Join(t.TempDir(), "race.db")
	db, err := InitDB(tmpPath, 768)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()
	vi := newVectorIndex("in-memory", db, 768)

	const N = 50
	const dim = 768
	// Each entity gets a one-hot unit vector: entity i has a 1.0
	// at dimension i, 0 everywhere else. After NormalizeVector
	// ||v||=1 is preserved, so cosine(e_i, e_i)=1.0 and
	// cosine(e_i, e_k)=0.0 for i≠k — meaning rank-1 Search with
	// embedding[i] uniquely returns entity i without tie noise.
	// Earlier (i+1)*(j+1)/10000.0 produced too-collinear vectors
	// (search rank-1 drifted to near-neighbour entities).
	embeddings := make([][]float32, N)
	for i := 0; i < N; i++ {
		embeddings[i] = make([]float32, dim)
		embeddings[i][i] = 1.0
	}

	var wg sync.WaitGroup
	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			entity := Entity{
				ID:        "e-" + strconv.Itoa(i),
				Category:  "world",
				Content:   "content-" + strconv.Itoa(i),
				Embedding: embeddings[i],
			}
			if err := StoreEntityWithEmbedding(db, vi, entity); err != nil {
				errCh <- fmt.Errorf("goroutine %d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("StoreEntityWithEmbedding: %v", err)
	}

	// Invariant 1: DB count == N.
	var dbCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE embedding IS NOT NULL`).Scan(&dbCount); err != nil {
		t.Fatalf("db count: %v", err)
	}
	if dbCount != N {
		t.Fatalf("DB entity count = %d, want %d", dbCount, N)
	}

	// Invariant 2: per-entity, Search with its own embedding returns
	// the matching entity as top-1. This proves the index carries
	// every entity the DB holds (for non-null embeddings) — the
	// invariant the new ordering preserves.
	for i := 0; i < N; i++ {
		ids, sErr := vi.Search(ictx, embeddings[i], 1)
		if sErr != nil {
			t.Errorf("Search[%d]: %v", i, sErr)
			continue
		}
		expected := "e-" + strconv.Itoa(i)
		if len(ids) == 0 || ids[0] != expected {
			t.Errorf("Search[%d] returned %v, top should be %q", i, ids, expected)
		}
	}
}

// TestStoreEntityRollbackOnSQLiteFailure verifies that when the
// SQLite INSERT fails (simulated by closing the DB before the call),
// vi.Store's prior update is rolled back via vi.Remove. The fix's
// promise: index never points to a row the DB doesn't actually hold,
// so a phantom index entry cannot propagate downstream.
//
// Without the rollback branch, vi.Store replaces the seed vec with
// the new vec and the failed SQLite step leaves it behind; the
// search below would still return "ghost" (cos ≈ 0.9 against the
// new vec). With rollback, vi.Remove deletes the entry — distinguish
// the with/without-rollback states deterministically.
func TestStoreEntityRollbackOnSQLiteFailure(t *testing.T) {
	db, vi := memDB(t)

	// Seed embedding: 768-dim to match memDB's default and avoid
	// the embed_dim-mismatch warning noise.
	seedEmb := make([]float32, 768)
	for i := range seedEmb {
		seedEmb[i] = float32(i+1) / 100000.0
	}
	if err := StoreEntityWithEmbedding(db, vi, Entity{
		ID:        "ghost",
		Category:  "world",
		Content:   "old",
		Embedding: seedEmb,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Distinct vec for the failing update attempt: if rollback is
	// skipped, this is what's left in the index.
	newEmb := make([]float32, 768)
	for i := range newEmb {
		newEmb[i] = float32(769-i) / 100000.0
	}

	// Close the DB so the *next* INSERT OR REPLACE fails. The
	// preceding vi.Store call still completes successfully because
	// the vector index is in-memory; the rollback path then issues
	// vi.Remove to undo it.
	db.Close()

	err := StoreEntityWithEmbedding(db, vi, Entity{
		ID:        "ghost",
		Category:  "world",
		Content:   "new",
		Embedding: newEmb,
	})
	if err == nil {
		t.Fatal("expected error from closed-DB insert, got nil")
	}

	// After rollback, "ghost" must NOT be in the index.
	query := make([]float32, 768)
	for i := range query {
		query[i] = 0.005
	}
	ids, sErr := vi.Search(ictx, query, 10)
	if sErr != nil {
		t.Fatalf("Search post-rollback: %v", sErr)
	}
	for _, id := range ids {
		if id == "ghost" {
			t.Errorf("index still contains 'ghost' after rollback (err=%v)", err)
		}
	}
}
