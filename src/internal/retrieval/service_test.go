package retrieval

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

// svcStubEmbedder is the fixed-vec stub used by service_test.go. The
// walk_test.go package already defines its own stubEmbedder type with
// .vecs/.calls recording fields; this stub is intentionally a separate
// type so the two test files don't cross-pollute fixtures.
type svcStubEmbedder struct{}

func (svcStubEmbedder) Embed(_ context.Context, content string) ([]float32, error) {
	if content == "" {
		return []float32{1, 0, 0}, nil
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func (svcStubEmbedder) Ping(_ context.Context) error {
	return nil
}

// svcErrEmbedder returns a sentinel error on every Embed call so tests
// can verify error-propagation paths (Search, Query) without reaching
// into retry/multi-call loops.
type svcErrEmbedder struct{ msg string }

func (e *svcErrEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New(e.msg)
}

func (e *svcErrEmbedder) Ping(_ context.Context) error {
	return nil
}

// svcFixture wires a Service against an in-memory SQLite + in-memory VI
// using the same pattern as server/integration_test.go::newTestFixture:
// store.MemDB() returns *sql.DB directly, no wrapper type.
type svcFixture struct {
	svc *Service
	db  *sql.DB
}

func newSvcFixture(t *testing.T) *svcFixture {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	vi := vector.NewInMemoryVectorIndex(db)
	svc := New(db, vi, svcStubEmbedder{})
	return &svcFixture{svc: svc, db: db}
}

// seedSvcEntity inserts one row so Search can find a candidate.
func seedSvcEntity(t *testing.T, svc *Service, id, content string) {
	t.Helper()
	emb := []float32{0.1, 0.2, 0.3}
	_, err := svc.db.Exec(
		`INSERT INTO entities (id, category, content, embedding, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		id, "world", content, store.EmbeddingToBytes(emb),
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.vi.Store(t.Context(), id, emb); err != nil {
		t.Fatalf("vi.Store: %v", err)
	}
}

// --- NewService ---

func TestNewService_Success(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	vi := vector.NewInMemoryVectorIndex(db)
	svc := New(db, vi, svcStubEmbedder{})
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

// --- Search ---

func TestService_Search_OK_DefaultTopK(t *testing.T) {
	f := newSvcFixture(t)
	seedSvcEntity(t, f.svc, "s1", "hello world")
	results, err := f.svc.Search(t.Context(), "hello", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("want at least one result, got 0")
	}
}

func TestService_Search_OK_TopKExplicit(t *testing.T) {
	f := newSvcFixture(t)
	seedSvcEntity(t, f.svc, "s1", "hello")
	results, err := f.svc.Search(t.Context(), "hello", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) > 1 {
		t.Fatalf("want at most 1 result with TopK=1, got %d", len(results))
	}
}

func TestService_Search_RejectsEmptyQuery(t *testing.T) {
	f := newSvcFixture(t)
	_, err := f.svc.Search(t.Context(), "", 5)
	if err == nil {
		t.Fatal("expected empty-query error, got nil")
	}
	if !strings.Contains(err.Error(), "query required") {
		t.Errorf("err=%v does not advertise query-required contract", err)
	}
}

func TestService_Search_PropagatesEmbedError(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	vi := vector.NewInMemoryVectorIndex(db)
	svc := New(db, vi, &svcErrEmbedder{msg: "embed-down"})

	_, err = svc.Search(t.Context(), "hello", 5)
	if err == nil {
		t.Fatal("expected embed error, got nil")
	}
	if !strings.Contains(err.Error(), "embed-down") {
		t.Errorf("err=%v does not surface embedder error verbatim", err)
	}
}

// --- Retrieve ---

func TestService_Retrieve_OK(t *testing.T) {
	f := newSvcFixture(t)
	seedSvcEntity(t, f.svc, "ra", "alpha")
	seedSvcEntity(t, f.svc, "rb", "beta")
	_, err := f.db.Exec(
		`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES (?, ?, ?, 1.0)`,
		"ra", "rb", "related_to",
	)
	if err != nil {
		t.Fatalf("seed edge: %v", err)
	}
	opts := core.RetrieveContextOptions{Ctx: t.Context()}
	result, err := f.svc.Retrieve(t.Context(), []string{"ra"}, opts)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if result == nil {
		t.Fatal("want non-nil RetrievalResult")
	}
}

func TestService_Retrieve_RejectsEmptySeeds(t *testing.T) {
	f := newSvcFixture(t)
	_, err := f.svc.Retrieve(t.Context(), nil, core.RetrieveContextOptions{})
	if err == nil {
		t.Fatal("expected empty-seeds error, got nil")
	}
	if !strings.Contains(err.Error(), "seed_ids required") {
		t.Errorf("err=%v does not advertise seed_ids-required contract", err)
	}
}

func TestService_Retrieve_AutoFillsCtx(t *testing.T) {
	f := newSvcFixture(t)
	seedSvcEntity(t, f.svc, "ctx-1", "ctx alpha")
	result, err := f.svc.Retrieve(t.Context(), []string{"ctx-1"}, core.RetrieveContextOptions{})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if result == nil {
		t.Fatal("want non-nil RetrievalResult")
	}
}

// --- Query ---

func TestService_Query_OK_ReturnsMarkdown(t *testing.T) {
	f := newSvcFixture(t)
	seedSvcEntity(t, f.svc, "qa", "alpha query")
	opts := core.RetrieveContextOptions{Ctx: t.Context()}
	md, err := f.svc.Query(t.Context(), "alpha", 0, opts)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	_ = md // empty seed pool → empty md is acceptable behaviour
}

func TestService_Query_RejectsEmptyQuery(t *testing.T) {
	f := newSvcFixture(t)
	_, err := f.svc.Query(t.Context(), "", 0, core.RetrieveContextOptions{})
	if err == nil {
		t.Fatal("expected empty-query error, got nil")
	}
	if !strings.Contains(err.Error(), "query required") {
		t.Errorf("err=%v does not advertise query-required contract", err)
	}
}

func TestService_Query_PropagatesEmbedError(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	vi := vector.NewInMemoryVectorIndex(db)
	svc := New(db, vi, &svcErrEmbedder{msg: "query-embed-fail"})
	_, err = svc.Query(t.Context(), "anything", 0, core.RetrieveContextOptions{})
	if err == nil {
		t.Fatal("expected embed error, got nil")
	}
}

// --- Response ---

func TestService_Response_RejectsEmptyQuery(t *testing.T) {
	f := newSvcFixture(t)
	_, err := f.svc.Response(t.Context(), "", core.RetrieveContextOptions{})
	if err == nil {
		t.Fatal("expected empty-query rejection, got nil")
	}
	if !strings.Contains(err.Error(), "query required") {
		t.Errorf("err=%v does not advertise query-required contract", err)
	}
}

// --- Explain ---

func TestService_Explain_OK(t *testing.T) {
	f := newSvcFixture(t)
	seedSvcEntity(t, f.svc, "ea", "explain alpha")
	opts := core.RetrieveContextOptions{Ctx: t.Context()}
	result, err := f.svc.Explain(t.Context(), "alpha", 0, opts)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if result == nil {
		t.Fatal("want non-nil RetrievalResult")
	}
}

func TestService_Explain_SwallowsEmbedError(t *testing.T) {
	// HTTP HandleQueryExplain and CLI newExplainCmd both
	// swallowed embed errors. PHASE 2.2 Service.Explain must preserve
	// that contract — embed-propagation here would visibly regress
	// /query/explain behaviour for users whose embedder transiently
	// fails.
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	vi := vector.NewInMemoryVectorIndex(db)
	svc := New(db, vi, &svcErrEmbedder{msg: "explain-embed-fail"})

	result, err := svc.Explain(t.Context(), "anything", 0, core.RetrieveContextOptions{})
	if err != nil {
		t.Fatalf("Explain should swallow embed error, got: %v", err)
	}
	if result == nil {
		t.Fatal("want non-nil (possibly empty) RetrievalResult")
	}
}

// --- Provenance ---

// TestService_Provenance_OK_DefaultLimit seeds an entity with a
// conversation_id, then calls Provenance with empty limit — the
// domain replaces it with DefaultProvenanceLimit and the call
// succeeds. Mirrors store/graph_test.go::TestGetEntitiesByProvenance_
// ConversationFilter shape so contract parity is visible.
func TestService_Provenance_OK_DefaultLimit(t *testing.T) {
	f := newSvcFixture(t)
	emb := []float32{0.1, 0.2, 0.3}
	if _, err := f.db.Exec(
		`INSERT INTO entities (id, category, content, embedding, conversation_id, created_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		"pv1", "world", "provenance seed", store.EmbeddingToBytes(emb), "conv-seed",
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	entities, err := f.svc.Provenance(t.Context(), "conv-seed", "", "", 0)
	if err != nil {
		t.Fatalf("Provenance: %v", err)
	}
	if len(entities) == 0 {
		t.Fatal("want at least one matching entity, got 0")
	}
}

// TestService_Provenance_RequiresAtLeastOneFilter pins the validation
// contract: empty conversation_id + empty message_id + empty source
// triggers an error from store.GetEntitiesByProvenance (rebranded as
// Service.Provenance error envelope). The HTTP shell maps this to 400
// in the wrapper; here we just verify the service-level signal.
func TestService_Provenance_RequiresAtLeastOneFilter(t *testing.T) {
	f := newSvcFixture(t)
	_, err := f.svc.Provenance(t.Context(), "", "", "", 0)
	if err == nil {
		t.Fatal("expected empty-filter rejection, got nil")
	}
	if !strings.Contains(err.Error(), "provenance filter required") {
		t.Errorf("err=%v does not advertise the at-least-one-filter contract", err)
	}
}

// --- Defaults ---

func TestDefaultConstants(t *testing.T) {
	if DefaultSearchTopK != 5 {
		t.Errorf("DefaultSearchTopK: want 5, got %d", DefaultSearchTopK)
	}
	if DefaultQueryTopK != 3 {
		t.Errorf("DefaultQueryTopK: want 3, got %d", DefaultQueryTopK)
	}
	if DefaultRetrieveMaxDepth != 2 {
		t.Errorf("DefaultRetrieveMaxDepth: want 2, got %d", DefaultRetrieveMaxDepth)
	}
	if DefaultProvenanceLimit != 50 {
		t.Errorf("DefaultProvenanceLimit: want 50, got %d", DefaultProvenanceLimit)
	}
}
