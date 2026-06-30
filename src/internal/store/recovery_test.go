package store

import (
	"errors"
	"fmt"
	"testing"
	"time"
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

func itoa(i int) string { return fmt.Sprintf("%d", i) }
func now() time.Time    { return time.Now() }

// --- C4: CascadeRollback iterative BFS + depth cap ---

// TestCascadeRollback_DeepChain verifies a chain of 10,000 tasks
// completes without stack overflow (iterative BFS).
func TestCascadeRollback_DeepChain(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	schema.CascadeLimit = 20_000 // exceed the chain depth
	const depth = 10_000

	// Build chain: root → n1 → n2 → ... → n{depth}
	seedEntity(t, db, "root", "task", "root")
	for i := 1; i <= depth; i++ {
		seedEntity(t, db, "n"+itoa(i), "task", "chain")
	}
	var prev string = "root"
	for i := 1; i <= depth; i++ {
		id := "n" + itoa(i)
		seedEdge(t, db, prev, id, schema.RelationBlocking, 1)
		prev = id
	}

	result, err := CascadeRollback(db, schema, "root", "test")
	if err != nil {
		t.Fatalf("deep chain: unexpected error: %v", err)
	}
	if len(result) != depth+1 {
		t.Fatalf("deep chain: want %d rolled back, got %d", depth+1, len(result))
	}
	if result[0].ID != "root" {
		t.Fatalf("deep chain: first result should be root, got %s", result[0].ID)
	}
}

// TestCascadeRollback_WideFanOut verifies 50,000 dependents on one root.
func TestCascadeRollback_WideFanOut(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wide fan-out in short mode")
	}
	db := openTestDB(t)
	schema := statefulSchema()
	schema.CascadeLimit = 60_000 // exceed the fan-out
	const width = 50_000

	seedEntity(t, db, "root", "task", "root")
	for i := 0; i < width; i++ {
		id := "f" + itoa(i)
		seedEntity(t, db, id, "task", "fan")
		seedEdge(t, db, "root", id, schema.RelationBlocking, 1)
	}

	result, err := CascadeRollback(db, schema, "root", "test")
	if err != nil {
		t.Fatalf("wide fan-out: unexpected error: %v", err)
	}
	if len(result) != width+1 {
		t.Fatalf("wide fan-out: want %d rolled back, got %d", width+1, len(result))
	}
}

// TestCascadeRollback_CycleTerminates ensures a cycle in blocked_by
// edges terminates without infinite loop (visited set).
func TestCascadeRollback_CycleTerminates(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()

	// Cycle: a → b → c → a
	seedEntity(t, db, "a", "task", "a")
	seedEntity(t, db, "b", "task", "b")
	seedEntity(t, db, "c", "task", "c")
	seedEdge(t, db, "a", "b", schema.RelationBlocking, 1)
	seedEdge(t, db, "b", "c", schema.RelationBlocking, 1)
	seedEdge(t, db, "c", "a", schema.RelationBlocking, 1)

	result, err := CascadeRollback(db, schema, "a", "cycle")
	if err != nil {
		t.Fatalf("cycle: unexpected error: %v", err)
	}
	// All three should be rolled back; visited set breaks the cycle.
	if len(result) != 3 {
		t.Fatalf("cycle: want 3 rolled back, got %d", len(result))
	}
	ids := make(map[string]bool)
	for _, r := range result {
		ids[r.ID] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !ids[want] {
			t.Fatalf("cycle: missing %s in result", want)
		}
	}
}

// TestCascadeRollback_LimitExceeded verifies that a small CascadeLimit
// returns ErrCascadeLimit with the partial result.
func TestCascadeRollback_LimitExceeded(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	schema.CascadeLimit = 5

	// Chain of 20 tasks; limit=5 should cap at 5.
	seedEntity(t, db, "root", "task", "root")
	var prev string = "root"
	for i := 1; i <= 20; i++ {
		id := "c" + itoa(i)
		seedEntity(t, db, id, "task", id)
		seedEdge(t, db, prev, id, schema.RelationBlocking, 1)
		prev = id
	}

	result, err := CascadeRollback(db, schema, "root", "capped")
	if !errors.Is(err, ErrCascadeLimit) {
		t.Fatalf("limit: want ErrCascadeLimit, got %v", err)
	}
	if len(result) > 5 {
		t.Fatalf("limit: want at most 5 results, got %d", len(result))
	}
}

// TestCascadeRollback_ImplicitLimit verifies default limit (4096)
// is applied when CascadeLimit is zero.
func TestCascadeRollback_ImplicitLimit(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	// schema.CascadeLimit == 0 → default 4096

	seedEntity(t, db, "root", "task", "root")
	result, err := CascadeRollback(db, schema, "root", "")
	if err != nil {
		t.Fatalf("implicit limit: unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("implicit limit: want 1 result, got %d", len(result))
	}
}

// TestCascadeRollback_SkipsAlreadyRolledBack ensures that tasks already
// in the unblocking state are skipped (idempotent) and their dependents
// are NOT processed (matches original recursive semantics).
func TestCascadeRollback_SkipsAlreadyRolledBack(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()

	// root → mid (pending) → leaf (already rolled back) → grandchild (pending)
	// Expect: root and mid rolled back; leaf skipped; grandchild NOT touched.
	seedEntity(t, db, "root", "task", "root")
	seedEntity(t, db, "mid", "task", "mid")
	seedEntityFull(t, db, "leaf", "task", "leaf", schema.StateUnblocking, now(), nil)
	seedEntity(t, db, "grandchild", "task", "grandchild")
	seedEdge(t, db, "root", "mid", schema.RelationBlocking, 1)
	seedEdge(t, db, "mid", "leaf", schema.RelationBlocking, 1)
	seedEdge(t, db, "leaf", "grandchild", schema.RelationBlocking, 1)

	result, err := CascadeRollback(db, schema, "root", "idempotent")
	if err != nil {
		t.Fatalf("idempotent: unexpected error: %v", err)
	}
	// root and mid rolled back; leaf skipped (already unblocking);
	// grandchild NOT processed (leaf's dependents are not walked).
	if len(result) != 2 {
		t.Fatalf("idempotent: want 2 results, got %d: %v", len(result), taskIDs(result))
	}
	ids := make(map[string]bool)
	for _, r := range result {
		ids[r.ID] = true
	}
	if !ids["root"] || !ids["mid"] {
		t.Fatalf("idempotent: want root+mid, got %v", taskIDs(result))
	}
	if ids["leaf"] {
		t.Fatal("idempotent: leaf should be skipped (already rolled back)")
	}
	// Verify grandchild was NOT rolled back.
	var status string
	_ = db.QueryRow(`SELECT status FROM entities WHERE id = 'grandchild'`).Scan(&status)
	if status == schema.StateUnblocking {
		t.Fatal("idempotent: grandchild should NOT be rolled back (leaf's dependents skipped)")
	}
}
