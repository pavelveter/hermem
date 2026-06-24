package store

import (
	"testing"
)

// --- FindRollbackTask ---

func TestFindRollbackTask_NoEdgeReturnsEmpty(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "a", "task", "alpha")
	got, err := FindRollbackTask(db, schema, "a")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "" {
		t.Fatalf("want \"\", got %q", got)
	}
}

func TestFindRollbackTask_FindsRecoversVia(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "a", "task", "alpha")
	seedEntity(t, db, "b", "task", "beta")
	seedEdge(t, db, "a", "b", schema.RelationRecovery, 1)

	got, err := FindRollbackTask(db, schema, "a")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "b" {
		t.Fatalf("want b, got %q", got)
	}
}

func TestFindRollbackTask_IgnoresOtherRelationTypes(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "a", "task", "alpha")
	seedEntity(t, db, "b", "task", "beta")
	seedEdge(t, db, "a", "b", "blocked_by", 1) // not recovers_via

	got, err := FindRollbackTask(db, schema, "a")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "" {
		t.Fatalf("non-recovers_via edge should be ignored, got %q", got)
	}
}

func TestFindRollbackTask_ReturnsFirstHit(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "a", "task", "alpha")
	seedEntity(t, db, "b", "task", "beta")
	seedEntity(t, db, "c", "task", "gamma")
	seedEdge(t, db, "a", "b", schema.RelationRecovery, 1)
	seedEdge(t, db, "a", "c", schema.RelationRecovery, 1)

	got, err := FindRollbackTask(db, schema, "a")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// LIMIT 1 — any non-empty answer is fine; just sanity-check it found SOMETHING.
	if got == "" {
		t.Fatal("expected a hit, got empty")
	}
}

// --- GenerateRecoveryPlan ---

func TestGenerateRecoveryPlan_NoChainReturnsEmpty(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "a", "task", "alpha")
	got, err := GenerateRecoveryPlan(db, schema, "a")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty plan, got %v", got)
	}
}

func TestGenerateRecoveryPlan_SingleHop(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "failed", "task", "the failure")
	seedEntity(t, db, "fix", "task", "the fix")
	seedEdge(t, db, "failed", "fix", schema.RelationRecovery, 1)

	got, err := GenerateRecoveryPlan(db, schema, "failed")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].ID != "fix" {
		t.Fatalf("want [fix], got %v", taskIDs(got))
	}
}

func TestGenerateRecoveryPlan_MultiHopOrderPreserved(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "failed", "task", "the failure")
	seedEntity(t, db, "fix1", "task", "first")
	seedEntity(t, db, "fix2", "task", "second")
	seedEdge(t, db, "failed", "fix1", schema.RelationRecovery, 1)
	seedEdge(t, db, "fix1", "fix2", schema.RelationRecovery, 1)

	got, err := GenerateRecoveryPlan(db, schema, "failed")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2-step plan, got %v", taskIDs(got))
	}
	if got[0].ID != "fix1" || got[1].ID != "fix2" {
		t.Fatalf("plan order: want [fix1 fix2], got %v", taskIDs(got))
	}
}

func TestGenerateRecoveryPlan_TerminatesOnNoRollback(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "failed", "task", "the failure")
	seedEntity(t, db, "step1", "task", "step")
	seedEntity(t, db, "leaf", "task", "leaf") // no outbound recovers_via
	seedEdge(t, db, "failed", "step1", schema.RelationRecovery, 1)
	seedEdge(t, db, "step1", "leaf", schema.RelationRecovery, 1)

	got, err := GenerateRecoveryPlan(db, schema, "failed")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2-step plan, got %v", taskIDs(got))
	}
}

func TestGenerateRecoveryPlan_CycleBreaksAtVisitedID(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "a", "task", "a")
	seedEntity(t, db, "b", "task", "b")
	seedEntity(t, db, "c", "task", "c")
	// a -> b -> c -> a (cycle)
	seedEdge(t, db, "a", "b", schema.RelationRecovery, 1)
	seedEdge(t, db, "b", "c", schema.RelationRecovery, 1)
	seedEdge(t, db, "c", "a", schema.RelationRecovery, 1)

	got, err := GenerateRecoveryPlan(db, schema, "a")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Without cycle-breaking this would loop until sql error or stack overflow.
	// With visited set: a, b, c visited; loop terminates cleanly.
	if len(got) != 2 {
		t.Fatalf("want 2-step plan [b, c] then break on cycle, got %v", taskIDs(got))
	}
	ids := taskIDs(got)
	if ids[0] != "b" || ids[1] != "c" {
		t.Fatalf("plan order: %v", ids)
	}
}

func TestGenerateRecoveryPlan_NonExistentStartReturnsEmpty(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	// No entity seeded; the chain walker should still terminate cleanly.
	got, err := GenerateRecoveryPlan(db, schema, "missing")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("missing start: want empty plan, got %v", got)
	}
}

// --- local helpers ---
// (No local helpers needed; sort.Strings is inlined where used.)
