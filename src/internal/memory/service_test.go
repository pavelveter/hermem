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

func (stubEmbedder) Ping(_ context.Context) error {
	return nil
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
	svc := New(db, vi, stubEmbedder{})
	return &memFixture{svc: svc, db: db, vi: vi}
}

// : seedEntity was previously used by AddEdge + Timeline tests
// (now migrated to edge/ + timeline/ pkgs). No remaining tests in this
// file need it. Helper removed to avoid an unused-symbol lint flag;
// future tests that need raw-row seeding should inline the insert.

// --- New ---

func TestNewService_Success(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	vi := vector.NewInMemoryVectorIndex(db)
	svc := New(db, vi, stubEmbedder{})
	if svc == nil {
		t.Fatal("New returned nil Service")
	}
}

// --- Store ---

func TestMemoryService_Store_OK(t *testing.T) {
	f := newMemFixture(t)
	req := core.StoreRequest{ID: "e1", Category: "world", Content: "hello", Embedding: []float32{0.1, 0.2, 0.3}}
	if err := f.svc.Store(t.Context(), req, core.DefaultSchemaConfig(false)); err != nil {
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
	err := f.svc.Store(t.Context(), req, core.DefaultSchemaConfig(false))
	if err == nil {
		t.Fatal("expected DomainError, got nil")
	}
	var de *core.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("want *core.DomainError, got %T: %v", err, err)
	}
	if de.Code != core.CodeInvalidSchema || de.Field != "category" || !strings.Contains(de.Message, "category") {
		t.Fatalf("want {code=invalid_schema, field=category}, got {code=%s, field=%s, msg=%s}", de.Code, de.Field, de.Message)
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
		err := f.svc.Store(t.Context(), req, core.DefaultSchemaConfig(false))
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
	if err := f.svc.StoreAndLink(t.Context(), req, core.DefaultSchemaConfig(false)); err != nil {
		t.Fatalf("StoreAndLink: %v", err)
	}
}

// --- Ingest ---
//
// : Ingest method lifted to src/internal/ingest.Service. The
// three Ingest_* tests that used to live here have moved to
// src/internal/ingest/service_test.go (TestService_Ingest_EmptyDialogReturnsError,
// TestService_Ingest_NilExtractorReturnsError, TestService_Ingest_HappyPath_NoEntities,
// TestService_Ingest_ExtractorErrorPropagates). The HTTP shell route
// /ingest moved from server/memory to server/ingest — URL contract
// unchanged.

// --- AddEdge ---
//
// : AddEdge method lifted to src/internal/edge.Service. The
// four AddEdge_* tests (OK, AutoCreate_OK, RejectsUnknownRelation,
// RejectsMissingFields) that used to live here have moved to
// src/internal/edge/service_test.go. The HTTP shell route /edge moved
// from server/memory to server/edge — URL contract unchanged.
// memory.Service.ErrInvalidSchema is RETAINED here for the Store
// category-validation path; edge pkg has its own edge.ErrInvalidSchema
// for the relation_type-validation path.

// --- Timeline ---
//
// : Timeline method + TimelineEntry type lifted to
// src/internal/timeline.Service. The two Timeline_* tests
// (EmptyDB, WithLimitAndOrder) that used to live here have moved to
// src/internal/timeline/service_test.go. The HTTP shell route /timeline
// moved from server/memory to server/timeline — URL contract unchanged.

// --- ErrInvalidSchema (now core.DomainError) ---
//
// : TestErrInvalidSchema_Error was removed from here because
// the relation_type field case moved to edge.ErrInvalidSchema. The
// category field case is exercised by the Store tests above (see
// TestMemoryService_Store_RejectsUnknownCategory) so the Error() string
// is implicitly covered. If a future refactor reintroduces a category
// field here, add the test back.
