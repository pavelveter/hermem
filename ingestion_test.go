package main

import (
	"context"
	"errors"
	"strings"
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
	db := memDB(t)
	defer db.Close()

	ext := &stubExtractor{resp: &ExtractionResult{Entities: []ExtractedEntity{
		{ID: "e1", Category: "world", Content: "Paris is capital of France"},
		{ID: "e2", Category: "world", Content: "Berlin is capital of Germany"},
	}}}
	emb := &stubEmbedder{}
	w := NewIngestionWorker(db, ext, emb, 0.99) // high threshold → no dedup
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
	db := memDB(t)
	defer db.Close()

	existingEmbed := []float32{0.4, 0.6, 0.8}
	if err := StoreEntityWithEmbedding(db, Entity{
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
	w := NewIngestionWorker(db, ext, emb, 0.99)
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
// inserted without a vector.
func TestProcessDialogEmbedderError(t *testing.T) {
	db := memDB(t)
	defer db.Close()

	embErr := errors.New("embedder down")
	ext := &stubExtractor{resp: &ExtractionResult{Entities: []ExtractedEntity{
		{ID: "x", Category: "world", Content: "x"},
	}}}
	emb := &stubEmbedder{err: embErr}
	w := NewIngestionWorker(db, ext, emb, 0.99)
	err := w.ProcessDialog(ictx, "d")
	if err == nil {
		t.Fatal("expected embedder error, got nil")
	}
	if !strings.Contains(err.Error(), "embedder down") {
		t.Errorf("error chain lost embedder cause: got %v", err)
	}
}

func TestProcessDialogExtractorError(t *testing.T) {
	db := memDB(t)
	defer db.Close()

	extErr := errors.New("ollama offline")
	ext := &stubExtractor{err: extErr}
	emb := &stubEmbedder{}
	w := NewIngestionWorker(db, ext, emb, 0.99)
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
	db := memDB(t)
	defer db.Close()

	ext := &stubExtractor{resp: &ExtractionResult{Entities: []ExtractedEntity{
		{ID: "m1", Category: "world", Content: "MemoryWorkerDrainsChannel fact"},
	}}}
	emb := &stubEmbedder{}
	ch := make(chan MemoryMessage, 4)
	ch <- MemoryMessage{Dialog: "hello"}
	close(ch)

	MemoryWorker(ictx, db, ext, emb, 0.99, ch)

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
	db := memDB(t)
	defer db.Close()

	ext := &stubExtractor{err: errors.New("first boom")}
	emb := &stubEmbedder{}
	ch := make(chan MemoryMessage, 2)
	ch <- MemoryMessage{Dialog: "first"}
	ch <- MemoryMessage{Dialog: "second"}
	close(ch)

	MemoryWorker(ictx, db, ext, emb, 0.99, ch)

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
	db := memDB(t)
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

	MemoryWorker(ictx, db, ext, emb, 0.99, ch)

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
	db := memDB(t)
	defer db.Close()

	existingEmbed := []float32{0.4, 0.6, 0.8}
	if err := StoreEntityWithEmbedding(db, Entity{
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
	w := NewIngestionWorker(db, ext, emb, 0.99)
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
