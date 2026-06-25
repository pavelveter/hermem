package memory

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// stubExtractor is a no-op LLM extractor used by tests that don't
// exercise the ingest happy-path (which would need a real extractor
// pulling JSON out of dialog). Tests that DO need a real extractor
// pass &stubExtractor{} — every ExtractEntities call returns an
// empty ExtractionResult, which is exactly what the IngestionWorker
// short-circuits on (no entities = no-op pipeline).
type stubExtractor struct{}

func (stubExtractor) ExtractEntities(_ context.Context, _ string) (*core.ExtractionResult, error) {
	return &core.ExtractionResult{}, nil
}

// stubEmbedder is a deterministic 3-dim embedder matching the
// server/integration_test.go stubEmbedder shape so any test that
// embeds entities lands on a known-good cosine surface.
type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, content string) ([]float32, error) {
	if content == "" {
		return []float32{1, 0, 0}, nil
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

// memFixture wires a memory.Service against an in-memory SQLite +
// in-memory vector index. Mirrors the construction pattern used in
// src/internal/server/integration_test.go::newTestFixture —
// store.MemDB() returns *sql.DB directly, no wrapper type.
type memFixture struct {
	svc *Service
	db  *sql.DB
	vi  *vector.InMemoryVectorIndex
}

func newMemFixture(t *testing.T) *memFixture {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	vi := vector.NewInMemoryVectorIndex(db)
	svc := New(db, vi, stubEmbedder{}, stubExtractor{})
	return &memFixture{svc: svc, db: db, vi: vi}
}

// seedEntity inserts a row directly so AddEdge / Timeline tests can
// reference pre-existing IDs without depending on Service.Store.
func (f *memFixture) seedEntity(t *testing.T, id, category, content string) {
	t.Helper()
	_, err := f.db.Exec(
		`INSERT INTO entities (id, category, content, embedding, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		id, category, content, []byte{},
	)
	if err != nil {
		t.Fatalf("seed entity %q: %v", id, err)
	}
}

// --- New ---

func TestNewService_Success(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	vi := vector.NewInMemoryVectorIndex(db)
	svc := New(db, vi, stubEmbedder{}, stubExtractor{})
	if svc == nil {
		t.Fatal("New returned nil Service")
	}
}

// --- Store ---

func TestMemoryService_Store_OK(t *testing.T) {
	f := newMemFixture(t)
	req := core.StoreRequest{ID: "e1", Category: "world", Content: "hello", Embedding: []float32{0.1, 0.2, 0.3}}
	if err := f.svc.Store(context.Background(), req, core.DefaultSchemaConfig(false)); err != nil {
		t.Fatalf("Store: %v", err)
	}
	// Verify the row landed in the DB.
	var cat string
	if err := f.db.QueryRow(`SELECT category FROM entities WHERE id = ?`, "e1").Scan(&cat); err != nil {
		t.Fatalf("query: %v", err)
	}
	if cat != "world" {
		t.Fatalf("want category=world, got %q", cat)
	}
}

func TestMemoryService_Store_RejectsUnknownCategory(t *testing.T) {
	f := newMemFixture(t)
	req := core.StoreRequest{ID: "e2", Category: "bogus", Content: "x"}
	err := f.svc.Store(context.Background(), req, core.DefaultSchemaConfig(false))
	if err == nil {
		t.Fatal("expected ErrInvalidSchema, got nil")
	}
	var ise *ErrInvalidSchema
	if !errors.As(err, &ise) {
		t.Fatalf("want ErrInvalidSchema, got %T: %v", err, err)
	}
	if ise.Field != "category" || ise.Value != "bogus" {
		t.Fatalf("want {category, bogus}, got {%s, %s}", ise.Field, ise.Value)
	}
}

func TestMemoryService_Store_RejectsMissingFields(t *testing.T) {
	f := newMemFixture(t)
	cases := []core.StoreRequest{
		{ID: "", Category: "world", Content: "x"},
		{ID: "e3", Category: "", Content: "x"},
		{ID: "e3", Category: "world", Content: ""},
	}
	for _, req := range cases {
		err := f.svc.Store(context.Background(), req, core.DefaultSchemaConfig(false))
		if err == nil {
			t.Fatalf("want error for missing fields, got nil for %+v", req)
		}
		if !strings.Contains(err.Error(), "id, category, content required") {
			t.Errorf("err=%v does not advertise missing-field contract", err)
		}
	}
}

func TestMemoryService_StoreAndLink_OK(t *testing.T) {
	f := newMemFixture(t)
	req := core.StoreRequest{ID: "store-link-1", Category: "world", Content: "linkable"}
	if err := f.svc.StoreAndLink(context.Background(), req, core.DefaultSchemaConfig(false)); err != nil {
		t.Fatalf("StoreAndLink: %v", err)
	}
}

// --- Ingest ---
//
// PHASE 3.4: Ingest method lifted to src/internal/ingest.Service. The
// three Ingest_* tests that used to live here have moved to
// src/internal/ingest/service_test.go (TestService_Ingest_EmptyDialogReturnsError,
// TestService_Ingest_NilExtractorReturnsError, TestService_Ingest_HappyPath_NoEntities,
// TestService_Ingest_ExtractorErrorPropagates). The HTTP shell route
// /ingest moved from server/memory to server/ingest — URL contract
// unchanged.

// --- AddEdge ---

func TestMemoryService_AddEdge_OK(t *testing.T) {
	f := newMemFixture(t)
	f.seedEntity(t, "src-ae", "world", "src")
	f.seedEntity(t, "tgt-ae", "world", "tgt")
	req := core.EdgeRequest{SourceID: "src-ae", TargetID: "tgt-ae", RelationType: "related_to", Weight: 1.0}
	if err := f.svc.AddEdge(context.Background(), req, core.DefaultSchemaConfig(false)); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = ? AND target_id = ?`, "src-ae", "tgt-ae").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 edge row, got %d", n)
	}
}

func TestMemoryService_AddEdge_AutoCreate_OK(t *testing.T) {
	f := newMemFixture(t)
	f.seedEntity(t, "src-ac", "world", "src")
	f.seedEntity(t, "tgt-ac", "world", "tgt")
	req := core.EdgeRequest{SourceID: "src-ac", TargetID: "tgt-ac", RelationType: "related_to", AutoCreate: true}
	if err := f.svc.AddEdge(context.Background(), req, core.DefaultSchemaConfig(false)); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
}

func TestMemoryService_AddEdge_RejectsUnknownRelation(t *testing.T) {
	f := newMemFixture(t)
	f.seedEntity(t, "src-r", "world", "src")
	f.seedEntity(t, "tgt-r", "world", "tgt")
	req := core.EdgeRequest{SourceID: "src-r", TargetID: "tgt-r", RelationType: "nonexistent"}
	err := f.svc.AddEdge(context.Background(), req, core.DefaultSchemaConfig(false))
	if err == nil {
		t.Fatal("expected ErrInvalidSchema, got nil")
	}
	var ise *ErrInvalidSchema
	if !errors.As(err, &ise) {
		t.Fatalf("want ErrInvalidSchema, got %T: %v", err, err)
	}
	if ise.Field != "relation_type" || ise.Value != "nonexistent" {
		t.Fatalf("want {relation_type, nonexistent}, got {%s, %s}", ise.Field, ise.Value)
	}
}

func TestMemoryService_AddEdge_RejectsMissingFields(t *testing.T) {
	f := newMemFixture(t)
	cases := []core.EdgeRequest{
		{SourceID: "", TargetID: "t", RelationType: "related_to"},
		{SourceID: "s", TargetID: "", RelationType: "related_to"},
		{SourceID: "s", TargetID: "t", RelationType: ""},
	}
	for _, req := range cases {
		err := f.svc.AddEdge(context.Background(), req, core.DefaultSchemaConfig(false))
		if err == nil {
			t.Fatalf("want error for missing fields, got nil for %+v", req)
		}
		if !strings.Contains(err.Error(), "source_id, target_id, relation_type required") {
			t.Errorf("err=%v does not advertise missing-field contract", err)
		}
	}
}

// --- Timeline ---

func TestMemoryService_Timeline_EmptyDB(t *testing.T) {
	f := newMemFixture(t)
	entries, err := f.svc.Timeline(context.Background(), 50)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 entries, got %d", len(entries))
	}
}

func TestMemoryService_Timeline_WithLimitAndOrder(t *testing.T) {
	f := newMemFixture(t)
	f.seedEntity(t, "tl-1", "world", "one")
	f.seedEntity(t, "tl-2", "world", "two")
	f.seedEntity(t, "tl-3", "world", "three")
	entries, err := f.svc.Timeline(context.Background(), 2)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.CreatedAt == nil {
			t.Errorf("entry %s missing created_at", e.ID)
		}
		if !strings.HasPrefix(e.ID, "tl-") {
			t.Errorf("entry %s not a seeded tl- row", e.ID)
		}
	}
}

// --- ErrInvalidSchema ---

func TestErrInvalidSchema_Error(t *testing.T) {
	e := &ErrInvalidSchema{Field: "category", Value: "bogus"}
	if got := e.Error(); got != "invalid category: bogus" {
		t.Fatalf("want 'invalid category: bogus', got %q", got)
	}
}
