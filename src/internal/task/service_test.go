package task

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// --- NewService ---

func TestNewService_Success(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	vi := vector.NewInMemoryVectorIndex(db)
	svc := New(db, &fixedEmbedder{}, vi)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

// --- Status ---

func TestService_Status_RejectsEmptyFields(t *testing.T) {
	f := newSvcFixture(t)
	if err := f.svc.Status(t.Context(), "", "completed", statefulSchema()); err == nil {
		t.Fatal("expected empty-id/status error, got nil")
	}
}

func TestService_Status_NotFound(t *testing.T) {
	f := newSvcFixture(t)
	err := f.svc.Status(t.Context(), "no-such-task", "completed", statefulSchema())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error from store, got: %v", err)
	}
}

// --- Executable ---

// TestService_Executable_EmptyDBReturnsEmpty pins the nil→empty
// normalization so the JSON envelope emits `[]` not `null`. Both
// stateful schema + goalID="" + zero entities in entities table
// should yield exactly zero-result slice, never nil.
func TestService_Executable_EmptyDBReturnsEmpty(t *testing.T) {
	f := newSvcFixture(t)
	tasks, err := f.svc.Executable(t.Context(), "", statefulSchema())
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	if tasks == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(tasks) != 0 {
		t.Errorf("want 0 tasks on empty DB, got %d", len(tasks))
	}
}

func TestService_Executable_NonStatefulSchemaReturnsEmpty(t *testing.T) {
	f := newSvcFixture(t)
	tasks, err := f.svc.Executable(t.Context(), "", core.DefaultSchemaConfig(false))
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("non-stateful schema should yield 0 tasks, got %d", len(tasks))
	}
}

// --- List ---

func TestService_List_EmptyDBReturnsEmpty(t *testing.T) {
	f := newSvcFixture(t)
	tasks, err := f.svc.List(t.Context(), "", "", statefulSchema())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if tasks == nil {
		t.Fatal("want non-nil empty slice, got nil")
	}
	if len(tasks) != 0 {
		t.Errorf("want 0 tasks on empty DB, got %d", len(tasks))
	}
}

// --- Show ---

func TestService_Show_RejectsEmptyID(t *testing.T) {
	f := newSvcFixture(t)
	_, _, _, err := f.svc.Show(t.Context(), "", statefulSchema())
	if err == nil {
		t.Fatal("expected empty-id error from domain, got nil")
	}
}

func TestService_Show_NotFound(t *testing.T) {
	f := newSvcFixture(t)
	_, _, _, err := f.svc.Show(t.Context(), "no-such-task", statefulSchema())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got: %v", err)
	}
}

// --- Dep ---

func TestService_Dep_RejectsEmptyIDs(t *testing.T) {
	f := newSvcFixture(t)
	if err := f.svc.Dep(t.Context(), "", "t2", "blocked_by", true); err == nil {
		t.Fatal("expected empty-source-id error, got nil")
	}
	if err := f.svc.Dep(t.Context(), "s1", "", "blocked_by", true); err == nil {
		t.Fatal("expected empty-target-id error, got nil")
	}
}

func TestService_Dep_AddCreatesEdge(t *testing.T) {
	f := newSvcFixture(t)
	seedTaskEntity(t, f.db, "t-src", "task", "pending", "do this first")
	seedTaskEntity(t, f.db, "t-dst", "task", "pending", "then do this")
	if err := f.svc.Dep(t.Context(), "t-src", "t-dst", "blocked_by", true); err != nil {
		t.Fatalf("Dep add: %v", err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = ? AND target_id = ? AND relation_type = ?`,
		"t-src", "t-dst", "blocked_by").Scan(&n); err != nil {
		t.Fatalf("edge count: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1 blocked_by edge, got %d", n)
	}
}

// --- Tree ---

// TestService_Tree_EmptyDBReturnsEmpty — empty store.GetRootTasks
// produces a nil []*TreeNode; store.RenderTaskTree explicitly returns
// "" on empty input. The service just threads that through. Assert
// no error and a stable empty string.
func TestService_Tree_EmptyDB(t *testing.T) {
	f := newSvcFixture(t)
	out, err := f.svc.Tree(t.Context(), "", statefulSchema())
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if out != "" {
		t.Errorf("want empty string on empty DB, got %q", out)
	}
}

// --- Create ---

func TestService_Create_RejectsEmptyContent(t *testing.T) {
	f := newSvcFixture(t)
	_, err := f.svc.Create(t.Context(), "t-new", "", nil, statefulSchema())
	if err == nil {
		t.Fatal("expected empty-content error, got nil")
	}
}

func TestService_Create_RejectsEmptyID(t *testing.T) {
	f := newSvcFixture(t)
	_, err := f.svc.Create(t.Context(), "", "do the thing", nil, statefulSchema())
	if err == nil {
		t.Fatal("expected empty-id error, got nil")
	}
}

func TestService_Create_RejectsNonStatefulSchema(t *testing.T) {
	f := newSvcFixture(t)
	_, err := f.svc.Create(t.Context(), "t-new", "do thing", nil, core.DefaultSchemaConfig(false))
	if err == nil || !strings.Contains(err.Error(), "no stateful category") {
		t.Errorf("expected no-stateful-category error, got: %v", err)
	}
}

func TestService_Create_Success(t *testing.T) {
	f := newSvcFixture(t)
	newID, err := f.svc.Create(t.Context(), "t-new", "do the thing", []string{}, statefulSchema())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if newID != "t-new" {
		t.Errorf("Create returned wrong id: %q", newID)
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM entities WHERE id = ?`, newID).Scan(&n); err != nil {
		t.Fatalf("entities count: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1 entity inserted, got %d", n)
	}
}

// --- RecoveryPlan ---

func TestService_RecoveryPlan_RejectsEmptyID(t *testing.T) {
	f := newSvcFixture(t)
	_, err := f.svc.RecoveryPlan(t.Context(), "", statefulSchema())
	if err == nil {
		t.Fatal("expected empty-id error from domain, got nil")
	}
}

// --- fixtures ---

type svcFixture struct {
	svc *Service
	db  *sql.DB
	vi  *vector.InMemoryVectorIndex
}

// fixedEmbedder returns a deterministic 3-dim vector for any input.
type fixedEmbedder struct{}

func (fixedEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

func (fixedEmbedder) Ping(_ context.Context) error {
	return nil
}

func newSvcFixture(t *testing.T) *svcFixture {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	vi := vector.NewInMemoryVectorIndex(db)
	svc := New(db, fixedEmbedder{}, vi)
	return &svcFixture{svc: svc, db: db, vi: vi}
}

func statefulSchema() core.SchemaConfig {
	s := core.DefaultSchemaConfig(true)
	s.AllowedCategories["task"] = true
	s.StatefulCategories["task"] = true
	s.ValidStates = map[string]bool{"pending": true, "running": true, "completed": true, "blocked": true}
	s.ValidStateOrder = []string{"pending", "running", "completed"}
	return s
}

func seedTaskEntity(t *testing.T, db *sql.DB, id, category, status, content string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO entities (id, category, content, status) VALUES (?, ?, ?, ?)`,
		id, category, content, status,
	); err != nil {
		t.Fatalf("seed task %s: %v", id, err)
	}
}
