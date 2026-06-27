package edge_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/edge"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// stubEmbedder is a deterministic 3-dim embedder matching the
// integration_test.go + memory/service_test.go stubEmbedder shape so
// any test that auto-creates an entity lands on the same cosine
// surface across all suites.
type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

func (stubEmbedder) Ping(_ context.Context) error {
	return nil
}

// newEdgeFixture wires an edge.Service against an in-memory SQLite +
// in-memory vector index. Mirrors the construction pattern in
// src/internal/server/integration_test.go::newTestFixture — same
// store.MemDB() backbone so the test surface stays portable.
func newEdgeFixture(t *testing.T) (*edge.Service, *sql.DB) {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	vi := vector.NewInMemoryVectorIndex(db)
	svc := edge.New(db, vi, stubEmbedder{})
	return svc, db
}

// seedEntity inserts a row directly so AddEdge tests can reference
// pre-existing IDs without depending on memory.Service.Store.
func seedEntity(t *testing.T, db *sql.DB, id, category, content string) {
	t.Helper()
	_, err := db.Exec(
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
	svc := edge.New(db, vi, stubEmbedder{})
	if svc == nil {
		t.Fatal("New returned nil Service")
	}
}

// --- AddEdge happy path ---

func TestEdgeService_AddEdge_OK(t *testing.T) {
	svc, db := newEdgeFixture(t)
	seedEntity(t, db, "edge-src-ok", "world", "src")
	seedEntity(t, db, "edge-tgt-ok", "world", "tgt")
	req := core.EdgeRequest{SourceID: "edge-src-ok", TargetID: "edge-tgt-ok", RelationType: "related_to", Weight: 1.0}
	if err := svc.AddEdge(context.Background(), req, core.DefaultSchemaConfig(false)); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	// Verify the row landed in the DB.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = ? AND target_id = ?`,
		"edge-src-ok", "edge-tgt-ok").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 edge row, got %d", n)
	}
}

func TestEdgeService_AddEdge_AutoCreate_OK(t *testing.T) {
	svc, db := newEdgeFixture(t)
	// Pre-seed both endpoints so the dispatcher doesn't auto-create
	// (covers the non-auto-create branch with AutoCreate=true still
	// succeeding because the endpoints already exist).
	seedEntity(t, db, "edge-ac-src", "world", "src")
	seedEntity(t, db, "edge-ac-tgt", "world", "tgt")
	req := core.EdgeRequest{SourceID: "edge-ac-src", TargetID: "edge-ac-tgt", RelationType: "related_to", AutoCreate: true}
	if err := svc.AddEdge(context.Background(), req, core.DefaultSchemaConfig(false)); err != nil {
		t.Fatalf("AddEdge AutoCreate: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = ? AND target_id = ?`,
		"edge-ac-src", "edge-ac-tgt").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 edge row, got %d", n)
	}
}

// --- AddEdge validation ---

func TestEdgeService_AddEdge_RejectsUnknownRelation(t *testing.T) {
	svc, db := newEdgeFixture(t)
	seedEntity(t, db, "edge-r-src", "world", "src")
	seedEntity(t, db, "edge-r-tgt", "world", "tgt")
	req := core.EdgeRequest{SourceID: "edge-r-src", TargetID: "edge-r-tgt", RelationType: "nonexistent"}
	err := svc.AddEdge(context.Background(), req, core.DefaultSchemaConfig(false))
	if err == nil {
		t.Fatal("expected DomainError, got nil")
	}
	var de *core.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("want *core.DomainError, got %T: %v", err, err)
	}
	if de.Code != core.CodeInvalidSchema || de.Field != "relation_type" {
		t.Fatalf("want {code=invalid_schema, field=relation_type}, got {code=%s, field=%s}", de.Code, de.Field)
	}
}

func TestEdgeService_AddEdge_RejectsMissingFields(t *testing.T) {
	svc, _ := newEdgeFixture(t)
	cases := []core.EdgeRequest{
		{SourceID: "", TargetID: "t", RelationType: "related_to"},
		{SourceID: "s", TargetID: "", RelationType: "related_to"},
		{SourceID: "s", TargetID: "t", RelationType: ""},
	}
	for _, req := range cases {
		err := svc.AddEdge(context.Background(), req, core.DefaultSchemaConfig(false))
		if err == nil {
			t.Fatalf("want error for missing fields, got nil for %+v", req)
		}
		if !strings.Contains(err.Error(), "source_id, target_id, relation_type required") {
			t.Errorf("err=%v does not advertise missing-field contract", err)
		}
	}
}

// --- DomainError (formerly ErrInvalidSchema) ---

func TestDomainError_InvalidSchema(t *testing.T) {
	de := core.NewInvalidSchemaError("relation_type", "nonexistent")
	if de.Code != core.CodeInvalidSchema || de.Field != "relation_type" {
		t.Fatalf("want {code=invalid_schema, field=relation_type}, got {code=%s, field=%s}", de.Code, de.Field)
	}
	if !strings.Contains(de.Error(), "invalid relation_type") {
		t.Fatalf("Error() should mention field + value, got %q", de.Error())
	}
}
