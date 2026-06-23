package main

import (
	"fmt"
	"math"
	"testing"
)

func TestEmbeddingBytesRoundTrip(t *testing.T) {
	original := []float32{-1.5, 0, 1e-3, 3.14, 1.7e38, -1.7e38, math.MaxFloat32}
	buf := EmbeddingToBytes(original)
	if len(buf) != len(original)*4 {
		t.Fatalf("byte length = %d, want %d", len(buf), len(original)*4)
	}
	got := BytesToEmbedding(buf)
	if len(got) != len(original) {
		t.Fatalf("decoded length = %d, want %d", len(got), len(original))
	}
	for i := range original {
		if got[i] != original[i] {
			t.Errorf("idx %d: got %v, want %v", i, got[i], original[i])
		}
	}
}

func TestBytesToEmbeddingRejectsMisalignedBuffer(t *testing.T) {
	if got := BytesToEmbedding([]byte{1, 2, 3}); got != nil {
		t.Errorf("BytesToEmbedding(non-multiple-of-4 buffer) = %v, want nil", got)
	}
}

func TestCosineIdentical(t *testing.T) {
	a := []float32{0.6, 0.8}
	if got := CosineSimilarity(a, a); got < 0.9999 {
		t.Errorf("cos(a,a) = %v, want >= 0.9999", got)
	}
}

func TestCosineOrthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	if got := CosineSimilarity(a, b); got != 0 {
		t.Errorf("cos(orthogonal) = %v, want 0", got)
	}
}

func TestCosineOpposite(t *testing.T) {
	a := []float32{0.6, 0.8}
	b := []float32{-0.6, -0.8}
	if got := CosineSimilarity(a, b); got > -0.9999 {
		t.Errorf("cos(opposite) = %v, want <= -0.9999", got)
	}
}

func TestCosineLenMismatchReturnsZero(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{1, 2, 3}
	if got := CosineSimilarity(a, b); got != 0 {
		t.Errorf("cos(len-mismatch) = %v, want 0", got)
	}
}

func TestCosineEmptyVectorReturnsZero(t *testing.T) {
	if got := CosineSimilarity([]float32{}, []float32{}); got != 0 {
		t.Errorf("cos(empty,empty) = %v, want 0", got)
	}
}

func TestCosineZeroVectorReturnsZero(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	if got := CosineSimilarity(a, b); got != 0 {
		t.Errorf("cos(zero-vec, real-vec) = %v, want 0", got)
	}
}

func TestStoreEntityInsertThenReadBack(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	entity := Entity{
		ID:        "vec-test-1",
		Category:  "world",
		Content:   "first",
		Embedding: []float32{0.5, 0.5, 0.5},
	}
	if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), entity); err != nil {
		t.Fatalf("store: %v", err)
	}
	var content string
	var blob []byte
	if err := db.QueryRow(`SELECT content, embedding FROM entities WHERE id = ?`, entity.ID).Scan(&content, &blob); err != nil {
		t.Fatalf("read-back: %v", err)
	}
	if content != "first" {
		t.Errorf("content = %q, want %q", content, "first")
	}
	got := BytesToEmbedding(blob)
	if len(got) != 3 {
		t.Fatalf("decoded length = %d, want 3", len(got))
	}
	for i, v := range entity.Embedding {
		if got[i] != v {
			t.Errorf("embedding[%d] = %v, want %v", i, got[i], v)
		}
	}
}

// TestStoreEntityUpsertReplacesSameID verifies the INSERT OR REPLACE
// semantics: same id, different content + embedding → row content
// updates, row count stays at 1.
func TestStoreEntityUpsertReplacesSameID(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{ID: "upsert-1", Category: "world", Content: "old", Embedding: []float32{0.1, 0.2}}); err != nil {
		t.Fatalf("store old: %v", err)
	}
	if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{ID: "upsert-1", Category: "opinion", Content: "new", Embedding: []float32{0.3, 0.4}}); err != nil {
		t.Fatalf("store new: %v", err)
	}
	var content, category string
	err := db.QueryRow(`SELECT content, category FROM entities WHERE id = ?`, "upsert-1").Scan(&content, &category)
	if err != nil {
		t.Fatalf("read-back: %v", err)
	}
	if content != "new" {
		t.Errorf("content after upsert = %q, want %q", content, "new")
	}
	if category != "opinion" {
		t.Errorf("category after upsert = %q, want opinion", category)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, "upsert-1").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("row count for upserted id = %d, want 1", n)
	}
}

// TestStoreEntityNullEmbeddingNotPersisted documents that an entity
// with no embedding round-trips as an entity with NULL embedding. The
// search path skips these rows (SearchByVector's `WHERE embedding IS
// NOT NULL`).
func TestStoreEntityNullEmbeddingNotPersisted(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{ID: "no-embed", Category: "world", Content: "no vector"}); err != nil {
		t.Fatalf("store: %v", err)
	}
	var blob []byte
	if err := db.QueryRow(`SELECT embedding FROM entities WHERE id = ?`, "no-embed").Scan(&blob); err != nil {
		t.Fatalf("read-back: %v", err)
	}
	if len(blob) != 0 {
		t.Errorf("embedding blob for nil vector = %d bytes, want 0", len(blob))
	}
}

func TestSearchByVectorRejectsEmptyQuery(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	if _, err := SearchByVector(db, vi, nil, 10); err == nil {
		t.Error("SearchByVector(nil) returned nil error")
	}
	if _, err := SearchByVector(db, vi, []float32{}, 10); err == nil {
		t.Error("SearchByVector(empty) returned nil error")
	}
}

func TestSearchByVectorOrdersBySimilarityDesc(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	for _, e := range []Entity{
		{ID: "near", Category: "world", Content: "near", Embedding: []float32{1, 0, 0}},
		{ID: "mid", Category: "world", Content: "mid", Embedding: []float32{0.5, 0.5, 0}},
		{ID: "far", Category: "world", Content: "far", Embedding: []float32{0, 1, 0}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store %s: %v", e.ID, err)
		}
	}
	results, err := SearchByVector(db, vi, []float32{1, 0, 0}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	wantOrder := []string{"near", "mid", "far"}
	for i, want := range wantOrder {
		if results[i].Entity.ID != want {
			t.Errorf("results[%d].ID = %q, want %q", i, results[i].Entity.ID, want)
		}
	}
	if !(results[0].Similarity >= results[1].Similarity && results[1].Similarity >= results[2].Similarity) {
		t.Errorf("non-monotonic ordering: %v, %v, %v",
			results[0].Similarity, results[1].Similarity, results[2].Similarity)
	}
}

func TestSearchByVectorTopKTrims(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
			ID: id, Category: "world", Content: id,
			Embedding: []float32{float32(i), float32(5 - i)},
		}); err != nil {
			t.Fatalf("store %s: %v", id, err)
		}
	}
	results, err := SearchByVector(db, vi, []float32{4, 0}, 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2 (top-K trim)", len(results))
	}
}

func TestSearchByVectorSkipsNullEmbeddingRows(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{ID: "with-vec", Category: "world", Content: "with vec", Embedding: []float32{0.5, 0.5, 0.5}}); err != nil {
		t.Fatalf("store with-vec: %v", err)
	}
	if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{ID: "without-vec", Category: "world", Content: "without vec"}); err != nil {
		t.Fatalf("store without-vec: %v", err)
	}
	results, err := SearchByVector(db, vi, []float32{0.5, 0.5, 0.5}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].Entity.ID != "with-vec" {
		t.Errorf("results = %v, want only [with-vec]", results)
	}
}

// ----- benchmarks ------------------------------------------------------

func BenchmarkSearchByVector100(b *testing.B)  { benchmarkSearchByVector(b, 100) }
func BenchmarkSearchByVector1000(b *testing.B) { benchmarkSearchByVector(b, 1000) }
func BenchmarkSearchByVector5000(b *testing.B) { benchmarkSearchByVector(b, 5000) }

func benchmarkSearchByVector(b *testing.B, n int) {
	db, err := InitDB(":memory:", 768)
	if err != nil {
		b.Fatalf("InitDB: %v", err)
	}
	vi := newVectorIndex("in-memory", db, 768)
	defer db.Close()

	for i := 0; i < n; i++ {
		emb := []float32{float32(i % 7), float32(i%11) / 11, float32(i%13) / 13}
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
			ID:        fmt.Sprintf("bench-%d", i),
			Category:  "world",
			Content:   fmt.Sprintf("fact-%d", i),
			Embedding: emb,
		}); err != nil {
			b.Fatalf("store %d: %v", i, err)
		}
	}
	q := []float32{0.3, 0.4, 0.5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := SearchByVector(db, vi, q, 10); err != nil {
			b.Fatalf("search: %v", err)
		}
	}
}
