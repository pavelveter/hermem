package task

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

func TestProperty_TaskStatus_InitialStateAfterCreate(t *testing.T) {
	f := newSvcFixture(t)
	schema := statefulSchema()

	id, err := f.svc.Create(t.Context(), "prop-1", "test task", nil, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	show, err := f.svc.Show(t.Context(), id, schema)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if show.Task.Status != "pending" {
		t.Fatalf("expected initial status 'pending', got %q", show.Task.Status)
	}
}

func TestProperty_TaskStatus_ValidTransitions(t *testing.T) {
	f := newSvcFixture(t)
	schema := statefulSchema()

	id, err := f.svc.Create(t.Context(), "prop-trans", "transition test", nil, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// pending → running → completed is the valid path
	for _, target := range []string{"running", "completed"} {
		if err := f.svc.Status(t.Context(), id, target, schema); err != nil {
			t.Fatalf("Status → %s: %v", target, err)
		}
		show, err := f.svc.Show(t.Context(), id, schema)
		if err != nil {
			t.Fatalf("Show: %v", err)
		}
		if show.Task.Status != target {
			t.Fatalf("after transition to %s, got status %q", target, show.Task.Status)
		}
	}
}

func TestProperty_TaskStatus_InvalidTransitionRejected(t *testing.T) {
	f := newSvcFixture(t)
	schema := statefulSchema()

	id, err := f.svc.Create(t.Context(), "prop-invalid", "reject test", nil, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Try an invalid status value (not in ValidStates at all)
	err = f.svc.Status(t.Context(), id, "bogus", schema)
	if err == nil {
		t.Fatal("expected error for invalid status value, got nil")
	}
}

func TestProperty_TaskShow_AfterCreate(t *testing.T) {
	f := newSvcFixture(t)
	schema := statefulSchema()

	id, err := f.svc.Create(t.Context(), "prop-show", "showable task", nil, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	show, err := f.svc.Show(t.Context(), id, schema)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if show.Task.Content != "showable task" {
		t.Fatalf("expected content 'showable task', got %q", show.Task.Content)
	}
	if show.Task.Category != "task" {
		t.Fatalf("expected category 'task', got %q", show.Task.Category)
	}
}

func TestProperty_TaskDep_Idempotent(t *testing.T) {
	f := newSvcFixture(t)
	seedTaskEntity(t, f.db, "dep-a", "task", "pending", "first")
	seedTaskEntity(t, f.db, "dep-b", "task", "pending", "second")

	// Add same edge twice — should not error
	if err := f.svc.Dep(t.Context(), "dep-a", "dep-b", "blocked_by", true); err != nil {
		t.Fatalf("Dep first: %v", err)
	}
	if err := f.svc.Dep(t.Context(), "dep-a", "dep-b", "blocked_by", true); err != nil {
		t.Fatalf("Dep second (idempotent): %v", err)
	}

	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = ? AND target_id = ? AND relation_type = ?`,
		"dep-a", "dep-b", "blocked_by").Scan(&n); err != nil {
		t.Fatalf("edge count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 edge after idempotent add, got %d", n)
	}
}

func TestProperty_TaskDep_RemoveIsIdempotent(t *testing.T) {
	f := newSvcFixture(t)
	seedTaskEntity(t, f.db, "rm-a", "task", "pending", "first")
	seedTaskEntity(t, f.db, "rm-b", "task", "pending", "second")

	if err := f.svc.Dep(t.Context(), "rm-a", "rm-b", "blocked_by", true); err != nil {
		t.Fatalf("Dep: %v", err)
	}
	// Remove once — should succeed
	if err := f.svc.Dep(t.Context(), "rm-a", "rm-b", "blocked_by", false); err != nil {
		t.Fatalf("Dep remove first: %v", err)
	}
	// Remove again — should also succeed (idempotent)
	if err := f.svc.Dep(t.Context(), "rm-a", "rm-b", "blocked_by", false); err != nil {
		t.Fatalf("Dep remove second: %v", err)
	}

	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM edges WHERE source_id = ? AND target_id = ? AND relation_type = ?`,
		"rm-a", "rm-b", "blocked_by").Scan(&n); err != nil {
		t.Fatalf("edge count: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 edges after double remove, got %d", n)
	}
}

func TestProperty_TaskCreate_ReturnsSameID(t *testing.T) {
	f := newSvcFixture(t)
	schema := statefulSchema()

	id, err := f.svc.Create(t.Context(), "prop-same-id", "task content", nil, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id != "prop-same-id" {
		t.Fatalf("expected returned ID 'prop-same-id', got %q", id)
	}
}

func TestProperty_TaskShow_InvalidID(t *testing.T) {
	f := newSvcFixture(t)
	schema := statefulSchema()

	_, err := f.svc.Show(t.Context(), "", schema)
	if err == nil {
		t.Fatal("expected error for empty ID, got nil")
	}
}

func TestProperty_TaskTree_Empty(t *testing.T) {
	f := newSvcFixture(t)
	schema := statefulSchema()

	tree, err := f.svc.Tree(t.Context(), "", schema)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if tree != "" {
		t.Fatalf("expected empty tree for empty DB, got %q", tree)
	}
}

func TestProperty_TaskRecoveryPlan_InvalidID(t *testing.T) {
	f := newSvcFixture(t)
	schema := statefulSchema()

	_, err := f.svc.RecoveryPlan(t.Context(), "", schema)
	if err == nil {
		t.Fatal("expected error for empty ID, got nil")
	}
}

// Verify schema config is well-formed.
func TestProperty_SchemaConfig_WellFormed(t *testing.T) {
	schema := statefulSchema()
	if !schema.StatefulEnabled {
		t.Fatal("expected StatefulEnabled=true")
	}
	if len(schema.ValidStateOrder) == 0 {
		t.Fatal("expected non-empty ValidStateOrder")
	}
	if len(schema.ValidStates) == 0 {
		t.Fatal("expected non-empty ValidStates")
	}
	// Every state in ValidStateOrder must be in ValidStates
	for _, s := range schema.ValidStateOrder {
		if !schema.ValidStates[s] {
			t.Fatalf("state %q in ValidStateOrder but not in ValidStates", s)
		}
	}
}

// Verify core.SchemaConfig satisfies the expected interface.
var _ core.SchemaConfig
