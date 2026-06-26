package store

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// statefulSchema returns a schema with `task` as the sole stateful category.
func statefulSchema() core.SchemaConfig {
	s := core.DefaultSchemaConfig(true)
	s.StatefulCategories = map[string]bool{"task": true}
	s.ValidStateOrder = []string{"pending", "in_progress", "done"}
	s.ValidStates = map[string]bool{"pending": true, "in_progress": true, "done": true}
	return s
}

func TestListTasks_ReturnsStatefulOnly(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "t1", "task", "first")
	seedEntity(t, db, "t2", "task", "second")
	seedEntity(t, db, "w1", "world", "non-task noise")

	tasks, err := ListTasks(db, schema, "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	gotIDs := taskIDs(tasks)
	if !reflectEqual(gotIDs, []string{"t1", "t2"}) {
		t.Fatalf("want only [t1 t2], got %v", gotIDs)
	}
}

func TestListTasks_FilterByStatus(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	past := time.Now().Add(-1 * time.Hour)
	seedEntityFull(t, db, "t1", "task", "a", "pending", past, nil)
	seedEntityFull(t, db, "t2", "task", "b", "done", past, nil)

	tasks, err := ListTasks(db, schema, "pending", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	gotIDs := taskIDs(tasks)
	if !reflectEqual(gotIDs, []string{"t1"}) {
		t.Fatalf("want [t1], got %v", gotIDs)
	}
}

func TestListTasks_NoStatefulCategoriesYieldsEmpty(t *testing.T) {
	db := openTestDB(t)
	s := core.DefaultSchemaConfig(false)
	seedEntity(t, db, "t1", "task", "x")
	tasks, err := ListTasks(db, s, "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("want empty, got %v", tasks)
	}
}

func TestGetTaskByID_HitAndMiss(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "t1", "task", "hello")

	got, err := GetTaskByID(db, schema, "t1")
	if err != nil {
		t.Fatalf("hit: %v", err)
	}
	if got.ID != "t1" || got.Content != "hello" || got.Category != "task" {
		t.Fatalf("unexpected: %+v", got)
	}

	if _, err := GetTaskByID(db, schema, "missing"); err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestGetTaskByID_RejectsNonStatefulCategory(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "w1", "world", "non-task")
	if _, err := GetTaskByID(db, schema, "w1"); err == nil {
		t.Fatal("expected error when category not in stateful_categories")
	}
}

func TestGetTaskWithRelations_ReturnsBlockedAndRecovers(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "a", "task", "alpha")
	seedEntity(t, db, "b", "task", "beta")
	seedEdge(t, db, "b", "a", schema.RelationBlocking, 1)
	seedEdge(t, db, "a", "b", schema.RelationRecovery, 1)

	entity, blocked, recovers, err := GetTaskWithRelations(db, schema, "a")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if entity.ID != "a" {
		t.Fatalf("entity id: %q", entity.ID)
	}
	if len(blocked) != 0 {
		t.Fatalf("a has no blockers, want 0, got %v", blocked)
	}
	if len(recovers) != 1 || recovers[0].SourceID != "a" || recovers[0].TargetID != "b" {
		t.Fatalf("recovers_via: want 1 a->b, got %v", recovers)
	}
}

func TestGetTasksByIDs_EmptyInputGivesEmptyMap(t *testing.T) {
	db := openTestDB(t)
	got, err := GetTasksByIDs(db, statefulSchema(), nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestGetTasksByIDs_PartialCoverage(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "t1", "task", "a")
	seedEntity(t, db, "t2", "task", "b")
	got, err := GetTasksByIDs(db, schema, []string{"t1", "missing"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got["t1"].Content != "a" {
		t.Fatalf("want just t1, got %v", got)
	}
}

func TestGetRootTasks_NoBlockersAreRoots(t *testing.T) {
	// With `root -> child` (root blocks child via blockers edge), `root` is
	// the only task with no incoming blockers edge, so it is the lone root.
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "root", "task", "r")
	seedEntity(t, db, "child", "task", "c")
	seedEdge(t, db, "root", "child", schema.RelationBlocking, 1)

	got, err := GetRootTasks(db, schema)
	if err != nil {
		t.Fatalf("roots: %v", err)
	}
	ids := taskIDs(got)
	if !reflectEqual(ids, []string{"root"}) {
		t.Fatalf("want [root (no blockers)], got %v", ids)
	}
}

func TestGetBlockedByAndRecoversVia_Direction(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "a", "task", "a")
	seedEntity(t, db, "b", "task", "b")
	seedEdge(t, db, "a", "b", schema.RelationBlocking, 1)
	seedEdge(t, db, "b", "a", schema.RelationRecovery, 1)

	blocked, err := GetBlockedBy(db, schema, "b")
	if err != nil {
		t.Fatalf("blocked: %v", err)
	}
	if len(blocked) != 1 || blocked[0].SourceID != "a" {
		t.Fatalf("GetBlockedBy should return edges where target_id=b: %v", blocked)
	}
	recov, err := GetRecoversVia(db, schema, "a")
	if err != nil {
		t.Fatalf("recovers: %v", err)
	}
	if len(recov) != 1 || recov[0].SourceID != "b" {
		t.Fatalf("GetRecoversVia should return edges where target_id=a: %v", recov)
	}

	blocked, _ = GetBlockedBy(db, schema, "a")
	if len(blocked) != 0 {
		t.Fatalf("a should not be blocked by anything: %v", blocked)
	}
}

func TestGetTaskTree_FromRoot_BuildsChildren(t *testing.T) {
	// BuildNode walks BLOCKERS-as-children. To get a 3-level tree starting
	// from `root`, root must be the most-blocked node: child -> root
	// (child blocks root), and grand -> child (grand blocks child).
	// grand is the leaf blocker (with no incoming blockers of its own).
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "root", "task", "root")
	seedEntity(t, db, "child", "task", "child")
	seedEntity(t, db, "grand", "task", "grand")
	seedEdge(t, db, "child", "root", schema.RelationBlocking, 1)
	seedEdge(t, db, "grand", "child", schema.RelationBlocking, 1)

	tree, err := GetTaskTree(db, schema, "root")
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("want single root tree, got %v", tree)
	}
	root := tree[0]
	if root.ID != "root" || len(root.Children) != 1 {
		t.Fatalf("root shape: id=%q children=%d", root.ID, len(root.Children))
	}
	if root.Children[0].ID != "child" || len(root.Children[0].Children) != 1 {
		t.Fatalf("child shape: %+v", root.Children[0])
	}
	if root.Children[0].Children[0].ID != "grand" {
		t.Fatalf("grand id: %q", root.Children[0].Children[0].ID)
	}
}

func TestGetTaskTree_TerminalLeavesHaveNoChildren(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "leaf", "task", "leaf")
	tree, err := GetTaskTree(db, schema, "leaf")
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if len(tree) != 1 || len(tree[0].Children) != 0 {
		t.Fatalf("leaf should be lone root: %+v", tree)
	}
}

func TestBuildNode_CycleAvoidedWithMarker(t *testing.T) {
	db := openTestDB(t)
	schema := statefulSchema()
	seedEntity(t, db, "x", "task", "x")
	seedEntity(t, db, "y", "task", "y")
	seedEdge(t, db, "x", "y", schema.RelationBlocking, 1)
	seedEdge(t, db, "y", "x", schema.RelationBlocking, 1)

	visited := map[string]bool{"x": true}
	node, err := BuildNode(db, schema, "x", visited)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if node.ID != "x" || node.Status != "cycle" || node.Content != "(cycle)" {
		t.Fatalf("cycle marker missing: %+v", node)
	}
}

func TestRenderTaskTree_BasicShape(t *testing.T) {
	root := &core.TreeNode{
		ID: "root", Content: "root task",
		Children: []*core.TreeNode{
			{ID: "leaf-a", Content: "leaf a"},
			{ID: "leaf-b", Content: "leaf b",
				Children: []*core.TreeNode{
					{ID: "deep", Content: "deep"},
				},
			},
		},
	}
	got := RenderTaskTree([]*core.TreeNode{root}, "")
	if !strings.Contains(got, "[root] root task") {
		t.Fatalf("missing root line: %q", got)
	}
	if !strings.Contains(got, "[deep] deep") {
		t.Fatalf("missing deep line: %q", got)
	}
}

func TestScanTaskEntities_PriorityScanned(t *testing.T) {
	db := openTestDB(t)
	_, _ = db.Exec(`INSERT INTO entities (id, category, content, status, priority) VALUES ('t1', 'task', 'p', 'pending', 7)`)
	rows, err := db.Query(`SELECT id, category, content, status, updated_at, COALESCE(priority, 0) FROM entities WHERE category IN ('task')`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	tasks, err := ScanTaskEntities(rows)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Priority != 7 {
		t.Fatalf("priority not picked up: %+v", tasks)
	}
}

// --- local helpers ---

func taskIDs(xs []core.Task) []string {
	ids := make([]string, len(xs))
	for i, x := range xs {
		ids[i] = x.ID
	}
	sort.Strings(ids)
	return ids
}

func reflectEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
