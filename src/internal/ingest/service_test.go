package ingest_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/ingest"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// stubExtractor satisfies core.LLMExtractor for tests that don't
// exercise the real Ollama / OpenAI HTTP clients. result + err
// together express every test path: empty result → no-op pipeline
// pass; non-nil err → dial/HTTP failure simulation.
type stubExtractor struct {
	result *core.ExtractionResult
	err    error
}

func (s *stubExtractor) ExtractEntities(_ context.Context, _ string) (*core.ExtractionResult, error) {
	return s.result, s.err
}

// newIngestFixture opens a fresh in-memory DB + vector index so
// per-test isolation holds. Mirrors the memory.Service + task.Service
// test fixture pattern.
func newIngestFixture(t *testing.T) (*sql.DB, *vector.InMemoryVectorIndex) {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("MemDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	vi := vector.NewInMemoryVectorIndex(db)
	return db, vi
}

func TestService_NewService_NotNil(t *testing.T) {
	db, vi := newIngestFixture(t)
	svc := ingest.NewService(db, vi, nil, &stubExtractor{result: &core.ExtractionResult{}})
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestService_Ingest_EmptyDialogReturnsError(t *testing.T) {
	db, vi := newIngestFixture(t)
	svc := ingest.NewService(db, vi, nil, &stubExtractor{result: &core.ExtractionResult{}})
	err := svc.Ingest(context.Background(), "", 0.5, core.DefaultSchemaConfig(false))
	if err == nil {
		t.Fatal("expected error from empty dialog, got nil")
	}
	if !strings.Contains(err.Error(), "dialog required") {
		t.Errorf("err message doesn't include 'dialog required': %v", err)
	}
}

func TestService_Ingest_NilExtractorReturnsError(t *testing.T) {
	db, vi := newIngestFixture(t)
	svc := ingest.NewService(db, vi, nil, nil)
	err := svc.Ingest(context.Background(), "user: hi", 0.5, core.DefaultSchemaConfig(false))
	if err == nil {
		t.Fatal("expected error from nil extractor, got nil")
	}
	if !strings.Contains(err.Error(), "no extractor wired") {
		t.Errorf("err message doesn't include 'no extractor wired': %v", err)
	}
}

func TestService_Ingest_HappyPath_NoEntities(t *testing.T) {
	db, vi := newIngestFixture(t)
	// Empty ExtractionResult + nil err is the no-op pipeline pass —
	// IngestionWorker iterates zero entities, returns nil. PHASE 3.4
	// preserves this short-circuit; if a regression reintroduces
	// an early-return guard for empty results, this test catches it.
	svc := ingest.NewService(db, vi, nil, &stubExtractor{result: &core.ExtractionResult{}})
	if err := svc.Ingest(context.Background(), "user: hi", 0.5, core.DefaultSchemaConfig(false)); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
}

func TestService_Ingest_ExtractorErrorPropagates(t *testing.T) {
	db, vi := newIngestFixture(t)
	svc := ingest.NewService(db, vi, nil, &stubExtractor{err: errors.New("dial boom")})
	err := svc.Ingest(context.Background(), "user: hi", 0.5, core.DefaultSchemaConfig(false))
	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
	// Root cause must surface under the "ingest: %w" wrap so the
	// operator can read the underlying HTTP / extractor message.
	if !strings.Contains(err.Error(), "dial boom") {
		t.Errorf("err message doesn't include dial root cause: %v", err)
	}
}
