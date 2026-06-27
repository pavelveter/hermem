package e2e

import (
	"context"
	"database/sql"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/graph"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/store"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
	"github.com/pavelveter/hermem/src/internal/testutil"
	"github.com/pavelveter/hermem/src/internal/vector"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return testutil.OpenTestDBSimple(t)
}

func newVectorIndex(db *sql.DB) *vector.InMemoryVectorIndex {
	return vector.NewInMemoryVectorIndex(db)
}

type stubEmbedder struct{}

func (e *stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

// --- E2E Flow: Store → Edge → Retrieve ---

func TestE2E_StoreEdgeRetrieve(t *testing.T) {
	db := openTestDB(t)
	vi := newVectorIndex(db)
	ctx := context.Background()
	schema := core.DefaultSchemaConfig(false)

	e1 := core.Entity{ID: "e2e-a", Category: "world", Content: "alpha entity", Embedding: []float32{1, 0, 0}}
	e2 := core.Entity{ID: "e2e-b", Category: "world", Content: "beta entity", Embedding: []float32{0, 1, 0}}
	if err := store.StoreEntityWithEmbedding(db, vi, schema, e1); err != nil {
		t.Fatalf("store a: %v", err)
	}
	if err := store.StoreEntityWithEmbedding(db, vi, schema, e2); err != nil {
		t.Fatalf("store b: %v", err)
	}

	if err := store.AddEdge(db, "e2e-a", "e2e-b", "related_to", 1.0); err != nil {
		t.Fatalf("add edge: %v", err)
	}

	// Verify via direct SQL query (GetTaskByID requires stateful categories)
	var content string
	err := db.QueryRow(`SELECT content FROM entities WHERE id = ?`, "e2e-a").Scan(&content)
	if err != nil {
		t.Fatalf("query entity: %v", err)
	}
	if content != "alpha entity" {
		t.Fatalf("content: want 'alpha entity', got %q", content)
	}

	// Verify edge
	edges, err := store.QueryEdges(db, `SELECT source_id, target_id, relation_type, COALESCE(weight, 1.0) FROM edges WHERE source_id = ? AND relation_type = ?`, "e2e-a", "related_to")
	if err != nil {
		t.Fatalf("query edges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d", len(edges))
	}
	if edges[0].TargetID != "e2e-b" {
		t.Fatalf("edge target: want e2e-b, got %s", edges[0].TargetID)
	}

	// Retrieve
	res, err := retrieval.RetrieveContext(db, []string{"e2e-a"}, core.RetrieveContextOptions{
		MaxDepth:      2,
		RankingWeight: core.RankingWeight{}.WithDefaults(),
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(res.SeedNodes) != 1 || res.SeedNodes[0].Entity.ID != "e2e-a" {
		t.Fatalf("seed: want [e2e-a], got %v", seedNodeIDs(res))
	}
	if len(res.WorldFacts) < 2 {
		t.Fatalf("want >=2 world facts, got %d", len(res.WorldFacts))
	}

	// Vector search
	results, err := vi.Search(ctx, []float32{1, 0, 0}, 5)
	if err != nil {
		t.Fatalf("vector search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("vector search returned no results")
	}

	// Remove
	if err := vi.Remove(ctx, []string{"e2e-a"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

// --- E2E Flow: Task lifecycle ---

func TestE2E_TaskLifecycle(t *testing.T) {
	db := openTestDB(t)
	vi := newVectorIndex(db)

	schema := core.DefaultSchemaConfig(true)
	schema.AllowedCategories["task"] = true
	schema.StatefulCategories["task"] = true
	schema.ValidStates = map[string]bool{"pending": true, "running": true, "completed": true}
	schema.ValidStateOrder = []string{"pending", "running", "completed"}
	schema.StateUnblocking = "completed"

	task1 := core.Entity{ID: "e2e-task1", Category: "task", Content: "first task", Embedding: []float32{1, 0, 0}}
	task2 := core.Entity{ID: "e2e-task2", Category: "task", Content: "second task", Embedding: []float32{0, 1, 0}}
	task3 := core.Entity{ID: "e2e-task3", Category: "task", Content: "third task", Embedding: []float32{0, 0, 1}}

	if err := store.StoreEntityWithEmbedding(db, vi, schema, task1); err != nil {
		t.Fatalf("store task1: %v", err)
	}
	if err := store.StoreEntityWithEmbedding(db, vi, schema, task2); err != nil {
		t.Fatalf("store task2: %v", err)
	}
	if err := store.StoreEntityWithEmbedding(db, vi, schema, task3); err != nil {
		t.Fatalf("store task3: %v", err)
	}

	// blocked_by direction: source blocks target → task1 blocks task2, task2 blocks task3
	if err := store.AddEdge(db, "e2e-task1", "e2e-task2", schema.RelationBlocking, 1.0); err != nil {
		t.Fatalf("add dep 1->2: %v", err)
	}
	if err := store.AddEdge(db, "e2e-task2", "e2e-task3", schema.RelationBlocking, 1.0); err != nil {
		t.Fatalf("add dep 2->3: %v", err)
	}

	// Root tasks = no edge targets them. task1 has no incoming blocked_by → task1 is root
	roots, err := store.GetRootTasks(db, schema)
	if err != nil {
		t.Fatalf("get root tasks: %v", err)
	}
	if len(roots) != 1 || roots[0].ID != "e2e-task1" {
		t.Fatalf("root task: want [e2e-task1], got %v", taskIDs(roots))
	}

	// Task tree
	tree, err := store.GetTaskTree(db, schema, "")
	if err != nil {
		t.Fatalf("get task tree: %v", err)
	}
	if len(tree) == 0 {
		t.Fatal("expected non-empty task tree")
	}
	treeStr := store.RenderTaskTree(tree, "")
	if treeStr == "" {
		t.Fatal("expected non-empty tree rendering")
	}

	// Set status
	if err := store.SetStatus(db, schema, "e2e-task3", "running"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	status, err := store.GetStatus(db, "e2e-task3")
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if status != "running" {
		t.Fatalf("status: want 'running', got %q", status)
	}

	// Get task with relations — blocked_by returns edges WHERE source_id = id (tasks blocked by this task)
	entity, blocked, _, err := store.GetTaskWithRelations(db, schema, "e2e-task2")
	if err != nil {
		t.Fatalf("get task with relations: %v", err)
	}
	if entity.ID != "e2e-task2" {
		t.Fatalf("entity: want e2e-task2, got %s", entity.ID)
	}
	// task2 blocks task3: edge source_id=task2 → target_id=task3
	if len(blocked) != 1 || blocked[0].TargetID != "e2e-task3" {
		t.Fatalf("blocked: want target=e2e-task3, got SourceID=%s TargetID=%s", blocked[0].SourceID, blocked[0].TargetID)
	}

	// List tasks filtered by status
	listed, err := store.ListTasks(db, schema, "running", "")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "e2e-task3" {
		t.Fatalf("listed running: want [e2e-task3], got %v", taskIDs(listed))
	}

	// Get tasks by IDs
	byIDs, err := store.GetTasksByIDs(db, schema, []string{"e2e-task1", "e2e-task3"})
	if err != nil {
		t.Fatalf("get tasks by ids: %v", err)
	}
	if len(byIDs) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(byIDs))
	}
}

// --- E2E Flow: Provenance + Contradictions ---

func TestE2E_ProvenanceAndContradictions(t *testing.T) {
	db := openTestDB(t)
	vi := newVectorIndex(db)

	// Store entities with provenance via raw SQL (StoreEntityWithEmbedding doesn't set provenance fields)
	blob1 := store.EmbeddingToBytes([]float32{1, 0, 0})
	_, err := db.Exec(`INSERT INTO entities (id, category, content, embedding, conversation_id, message_id, source, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		"p1", "world", "from conversation", blob1, "conv-1", "msg-1", "chat")
	if err != nil {
		t.Fatalf("insert p1: %v", err)
	}

	blob2 := store.EmbeddingToBytes([]float32{0, 1, 0})
	_, err = db.Exec(`INSERT INTO entities (id, category, content, embedding, conversation_id, message_id, created_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		"p2", "world", "from message", blob2, "conv-1", "msg-2")
	if err != nil {
		t.Fatalf("insert p2: %v", err)
	}

	// Sync vector index
	if err := vi.Store(context.Background(), "p1", []float32{1, 0, 0}); err != nil {
		t.Fatalf("vi store p1: %v", err)
	}
	if err := vi.Store(context.Background(), "p2", []float32{0, 1, 0}); err != nil {
		t.Fatalf("vi store p2: %v", err)
	}

	// Query by provenance
	entities, err := store.GetEntitiesByProvenance(db, "conv-1", "", "", 50)
	if err != nil {
		t.Fatalf("provenance query: %v", err)
	}
	if len(entities) != 2 {
		t.Fatalf("want 2 entities from conv-1, got %d", len(entities))
	}

	// Add a contradicts edge
	if err := store.AddEdge(db, "p1", "p2", "contradicts", 1.0); err != nil {
		t.Fatalf("add contradicts: %v", err)
	}

	// Query contradictions
	pairs, err := store.GetContradictions(db, "")
	if err != nil {
		t.Fatalf("contradictions: %v", err)
	}
	if len(pairs) == 0 {
		t.Fatal("expected at least 1 contradiction pair")
	}
	if pairs[0].SourceID != "p1" || pairs[0].TargetID != "p2" {
		t.Fatalf("contradiction: want p1->p2, got %s->%s", pairs[0].SourceID, pairs[0].TargetID)
	}

	// Retrieve with ranking
	res, err := retrieval.RetrieveContext(db, []string{"p1", "p2"}, core.RetrieveContextOptions{
		MaxDepth: 1, RankingWeight: core.RankingWeight{}.WithDefaults(),
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(res.SeedNodes) != 2 {
		t.Fatalf("want 2 seed nodes, got %d", len(res.SeedNodes))
	}
}

// --- E2E Flow: Multi-hop retrieval ---

func TestE2E_MultiHopRetrieval(t *testing.T) {
	db := openTestDB(t)
	vi := newVectorIndex(db)
	embed := &stubEmbedder{}
	schema := core.DefaultSchemaConfig(false)

	storeEntity(t, db, vi, schema, core.Entity{ID: "mh1", Category: "world", Content: "seed", Embedding: []float32{1, 0, 0}})
	storeEntity(t, db, vi, schema, core.Entity{ID: "mh2", Category: "world", Content: "hop1", Embedding: []float32{0, 1, 0}})
	storeEntity(t, db, vi, schema, core.Entity{ID: "mh3", Category: "world", Content: "hop2", Embedding: []float32{0, 0, 1}})
	storeEntity(t, db, vi, schema, core.Entity{ID: "mh-disconnected", Category: "world", Content: "far away", Embedding: []float32{0.5, 0.5, 0.5}})
	mustAddEdge(t, db, "mh1", "mh2", "related_to")
	mustAddEdge(t, db, "mh2", "mh3", "related_to")

	// Single-hop: stays within subgraph
	res, err := retrieval.MultiHopRetrieveContext(db, vi, embed, []string{"mh1"}, core.RetrieveContextOptions{
		MaxDepth: 2, RankingWeight: core.RankingWeight{}.WithDefaults(), MultiHopCount: 1,
	})
	if err != nil {
		t.Fatalf("single-hop: %v", err)
	}
	// WorldFacts should include seeded entities
	contents := factContents(res.WorldFacts)
	if !contains(contents, "seed") || !contains(contents, "hop1") || !contains(contents, "hop2") {
		t.Fatalf("single-hop facts: want seed,hop1,hop2 got %v", contents)
	}
	if contains(contents, "far away") {
		t.Fatal("single-hop: should NOT reach disconnected entity")
	}
}

// --- E2E Flow: Graph integrity ---

func TestE2E_GraphIntegrity(t *testing.T) {
	db := openTestDB(t)
	vi := newVectorIndex(db)
	schema := core.DefaultSchemaConfig(false)

	storeEntity(t, db, vi, schema, core.Entity{ID: "gi1", Category: "world", Content: "a", Embedding: []float32{1, 0, 0}})
	storeEntity(t, db, vi, schema, core.Entity{ID: "gi2", Category: "world", Content: "b", Embedding: []float32{0, 1, 0}})
	storeEntity(t, db, vi, schema, core.Entity{ID: "gi3", Category: "world", Content: "c", Embedding: []float32{0, 0, 1}})
	mustAddEdge(t, db, "gi1", "gi2", "related_to")
	mustAddEdge(t, db, "gi2", "gi3", "related_to")

	// Connected components
	comps, err := store.FindConnectedComponents(db, 2)
	if err != nil {
		t.Fatalf("components: %v", err)
	}
	if len(comps) != 1 {
		t.Fatalf("want 1 component (size >= 2), got %d", len(comps))
	}
	if comps[0].Size != 3 {
		t.Fatalf("component size: want 3, got %d", comps[0].Size)
	}

	// Communities (Louvain)
	communities, _, err := store.DetectCommunities(db, 10)
	if err != nil {
		t.Fatalf("communities: %v", err)
	}
	if len(communities) == 0 {
		t.Fatal("expected at least 1 community")
	}

	// Verify graph — PHASE 3.9: moved from algo.VerifyGraph into graph.Service.Verify
	report, err := graph.New(db).Verify(context.Background(), schema, 3)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(report.Issues) > 0 {
		t.Logf("verify issues: %v", report.Issues)
	}

	// Cycle detection
	if err := store.AddEdge(db, "gi3", "gi1", "related_to", 1.0); err == nil {
		t.Fatal("expected cycle detection error for gi3->gi1")
	}
}

// --- E2E Flow: Temporal retrieval ---

func TestE2E_TemporalRetrieval(t *testing.T) {
	db := openTestDB(t)
	vi := newVectorIndex(db)
	schema := core.DefaultSchemaConfig(false)

	storeEntity(t, db, vi, schema, core.Entity{ID: "tmp1", Category: "world", Content: "recent", Embedding: []float32{1, 0, 0}})
	storeEntity(t, db, vi, schema, core.Entity{ID: "tmp2", Category: "world", Content: "old", Embedding: []float32{0, 1, 0}})

	_, err := retrieval.RetrieveContext(db, []string{"tmp1", "tmp2"}, core.RetrieveContextOptions{
		MaxDepth: 1, RankingWeight: core.RankingWeight{}.WithDefaults(),
	})
	if err != nil {
		t.Fatalf("temporal retrieve: %v", err)
	}
}

// --- E2E Flow: Agent loop ---

func TestE2E_AgentLoop(t *testing.T) {
	db := openTestDB(t)
	vi := newVectorIndex(db)

	schema := core.DefaultSchemaConfig(true)
	schema.AllowedCategories["task"] = true
	schema.StatefulCategories["task"] = true
	schema.ValidStates = map[string]bool{"pending": true, "running": true, "completed": true}
	schema.ValidStateOrder = []string{"pending", "running", "completed"}
	schema.StateUnblocking = "completed"

	// Create task chain where task2 blocked_by task1 (task1 blocks task2)
	storeEntity(t, db, vi, schema, core.Entity{ID: "al-task1", Category: "task", Content: "do first", Embedding: []float32{1, 0, 0}})
	storeEntity(t, db, vi, schema, core.Entity{ID: "al-task2", Category: "task", Content: "do second", Embedding: []float32{0, 1, 0}})
	mustAddEdge(t, db, "al-task1", "al-task2", schema.RelationBlocking)

	// First task (no blockers) should be executable. PHASE 2.4:
	// retrieval.GetExecutableTasks migrated to internal/task/service.go.
	tasks, err := taskdomain.New(db, nil, nil).Executable(context.Background(), "", schema)
	if err != nil {
		t.Fatalf("get executable: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected at least 1 executable task")
	}

	// Mark first task complete so second becomes executable
	if err := store.SetStatus(db, schema, "al-task1", "completed"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	tasks2, err := taskdomain.New(db, nil, nil).Executable(context.Background(), "", schema)
	if err != nil {
		t.Fatalf("get executable after complete: %v", err)
	}
	if !containsEntity(tasks2, "al-task2") {
		t.Fatalf("task2 should be executable after task1 completes, got %v", tasks2)
	}
}

// --- Helpers ---

func storeEntity(t *testing.T, db *sql.DB, vi *vector.InMemoryVectorIndex, schema core.SchemaConfig, e core.Entity) {
	t.Helper()
	if err := store.StoreEntityWithEmbedding(db, vi, schema, e); err != nil {
		t.Fatalf("store %s: %v", e.ID, err)
	}
}

func mustAddEdge(t *testing.T, db *sql.DB, src, dst, rel string) {
	t.Helper()
	if err := store.AddEdge(db, src, dst, rel, 1.0); err != nil {
		t.Fatalf("add edge %s->%s: %v", src, dst, err)
	}
}

func seedNodeIDs(r *core.RetrievalResult) []string {
	out := make([]string, len(r.SeedNodes))
	for i, n := range r.SeedNodes {
		out[i] = n.Entity.ID
	}
	return out
}

func factContents(facts []core.RetrievedFact) []string {
	out := make([]string, len(facts))
	for i, f := range facts {
		out[i] = f.Content
	}
	return out
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func taskIDs(tasks []core.Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.ID
	}
	return out
}

func containsEntity(tasks []core.Task, id string) bool {
	for _, t := range tasks {
		if t.ID == id {
			return true
		}
	}
	return false
}
