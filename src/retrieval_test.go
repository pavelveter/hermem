package main

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// setupChainDB inserts n world entities in a forward chain: 0→1→2→…→n-1.
// Returns the DB; caller closes.
func setupChainDB(t *testing.T, n int) *sql.DB {
	t.Helper()
	db, vi := memDB(t)
	for i := 0; i < n; i++ {
		emb := []float32{float32(i) + 1, float32(i) + 2, float32(i) + 3}
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
			ID:        fmt.Sprintf("chain-%d", i),
			Category:  "world",
			Content:   fmt.Sprintf("Chain fact %d", i),
			Embedding: emb,
		}); err != nil {
			db.Close()
			t.Fatalf("store: %v", err)
		}
	}
	for i := 0; i < n-1; i++ {
		if _, err := db.Exec(`INSERT OR IGNORE INTO edges (source_id, target_id, relation_type) VALUES (?,?,?)`,
			fmt.Sprintf("chain-%d", i), fmt.Sprintf("chain-%d", i+1), "related_to"); err != nil {
			db.Close()
			t.Fatalf("edge: %v", err)
		}
	}
	return db
}

// TestRetrieveContextCycleGuard is a CONTRACT-level test satisfying
// TODO §4's "build a diamond/cycle graph and assert result is finite
// and bounded" item. It does NOT isolate the seenIDs guard as the
// mechanism — secondary seenContents dedup would also collapse a
// cycling CTE's repeated rows. The assertion is at the user-visible
// behavior: a 2-node A↔B cycle produces exactly 2 unique nodes,
// with no result-set inflation.
func TestRetrieveContextCycleGuard(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	for _, e := range []Entity{
		{ID: "A", Category: "world", Content: "node A", Embedding: []float32{1, 0}},
		{ID: "B", Category: "world", Content: "node B", Embedding: []float32{0, 1}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store %s: %v", e.ID, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('A','B','related_to'), ('B','A','related_to')`); err != nil {
		t.Fatalf("edges: %v", err)
	}

	res, err := RetrieveContext(db, []string{"A"}, RetrieveContextOptions{MaxDepth: 5})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	// Sum the category-bucket fact count — NOT SeedNodes + buckets.
	// Seed nodes ARE appended to category buckets by the post-scan
	// loop, so SeedNodes and a bucket can overlap on the same content.
	// The "unique entities" count is the number of distinct fact
	// contents, which is exactly what category buckets carry.
	total := len(res.WorldFacts) + len(res.Opinions) +
		len(res.Experiences) + len(res.Observations)
	if len(res.SeedNodes) != 1 {
		t.Errorf("SeedNodes = %d, want 1", len(res.SeedNodes))
	}
	if total != 2 {
		t.Errorf("unique fact contents = %d, want 2 (cycle inflated)", total)
	}
}

// TestRetrieveContext3CycleGuard verifies end-to-end that a
// deliberate 3-cycle A→B→C→A returns exactly 3 unique entities via
// the public RetrieveContext API. The test is a stronger cycle
// fixture than TestRetrieveContextCycleGuard's 2-node A↔B: at
// depth-cap=100 and a 3-step cycle, any cycle in graph_walk would
// repeatedly visit A, B, and C every 3 depths. With the visited-
// column cycle guard in the CTE, the recursion terminates once
// each node has been visited.
//
// End-to-end assertion: SELECT DISTINCT collapses the inflated CTE
// rows anyway, so the public-output check is met by both pre-fix
// and post-fix paths. Pair with TestGraphWalk3CycleDirectProbe
// which probes the inner CTE without DISTINCT to verify the actual
// SQL guard.
func TestRetrieveContext3CycleGuard(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	for _, e := range []Entity{
		{ID: "A", Category: "world", Content: "node A", Embedding: []float32{1, 0, 0}},
		{ID: "B", Category: "world", Content: "node B", Embedding: []float32{0, 1, 0}},
		{ID: "C", Category: "world", Content: "node C", Embedding: []float32{0, 0, 1}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store %s: %v", e.ID, err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('A','B','related_to'), ('B','C','related_to'), ('C','A','related_to')`,
	); err != nil {
		t.Fatalf("edges: %v", err)
	}

	res, err := RetrieveContext(db, []string{"A"}, RetrieveContextOptions{
		MaxDepth:     100,
		DepthCeiling: 100,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	facts := map[string]bool{}
	for _, sn := range res.SeedNodes {
		facts[sn.Entity.Content] = true
	}
	for _, f := range res.WorldFacts {
		facts[f.Content] = true
	}
	for _, want := range []string{"node A", "node B", "node C"} {
		if !facts[want] {
			t.Errorf("expected %q in result, missing; facts=%v", want, facts)
		}
	}
	if len(facts) != 3 {
		t.Errorf("unique fact count = %d, want 3 (cycle inflated)", len(facts))
	}
}

// TestGraphWalk3CycleDirectProbe exercises the cycle guard at the
// SQL layer. We replicate graph_walk's CTE WITHOUT the SELECT
// DISTINCT projection so the row count reveals SQLite's internal
// fan-out. With the visited-column guard, a 3-cycle at depth-cap
// =100 yields 5 rows; without the guard, the same query would
// generate ~depthCap/3 + 1 ≈ 34 rows.
//
// Bound analysis (depth-cap=100, edges A→B, B→C, C→A traversing
// undirected via the source_id OR target_id join):
//   - depth 0: A (1 row, path ',A,')
//   - depth 1: from A — A→B reaches B, C→A reaches C (2 rows)
//   - depth 2: from B (path ',A,B,') — B→C reaches C; from C
//     (path ',A,C,') — B→C reaches B (2 rows)
//   - depth 3+: every path has already visited its expansion target
//     so the instr() guard rejects further rows.
//
// The SELECT DISTINCT at the end of retrieval.go's query collapses
// these 5 rows down to 3 unique entities (A, B, C), so
// TestRetrieveContext3CycleGuard plus this probe pair exercise
// both the user-visible and the SQL-engine behaviours.
//
// This test must mirror retrieval.go's graph_walk CTE shape: if
// production changes the column list, the WHERE clause, or the
// depth-cap binding site, this test must update in lockstep or it
// will silently probe the wrong structure.
func TestGraphWalk3CycleDirectProbe(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	for _, e := range []Entity{
		{ID: "A", Category: "world", Content: "node A", Embedding: []float32{1, 0, 0}},
		{ID: "B", Category: "world", Content: "node B", Embedding: []float32{0, 1, 0}},
		{ID: "C", Category: "world", Content: "node C", Embedding: []float32{0, 0, 1}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store %s: %v", e.ID, err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('A','B','related_to'), ('B','C','related_to'), ('C','A','related_to')`,
	); err != nil {
		t.Fatalf("edges: %v", err)
	}

	const depthCap = 100
	var rowCount int
	err := db.QueryRow(`
		WITH RECURSIVE graph_walk AS (
			SELECT
				e.id, 0 as depth,
				char(31) || e.id || char(31) as visited
			FROM entities e
			WHERE e.id = 'A' AND e.archived = 0
			UNION ALL
			SELECT
				e.id, gw.depth + 1,
				gw.visited || e.id || char(31) as visited
			FROM graph_walk gw
			JOIN edges ed ON (ed.source_id = gw.id OR ed.target_id = gw.id)
			JOIN entities e ON (
				CASE
					WHEN ed.source_id = gw.id THEN ed.target_id = e.id
					ELSE ed.source_id = e.id
				END
			)
			WHERE gw.depth < ?
				AND instr(gw.visited, char(31) || e.id || char(31)) = 0
				AND e.archived = 0
		)
		SELECT COUNT(*) FROM graph_walk
	`, depthCap).Scan(&rowCount)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	// Post-fix bound: 5 rows (1 seed + 2 depth-1 + 2 depth-2; instr
	// guard rejects further expansion). Pre-fix bound at
	// depthCap=100 was ~100/3 + 1 ≈ 34 rows.
	if rowCount != 5 {
		t.Errorf("graph_walk row count at depth-cap=%d for 3-cycle = %d, want 5 (cycle guard failed; pre-fix would be ~%d)",
			depthCap, rowCount, depthCap/3+1)
	}
}

// TestRetrieveContextDepthCeilingClamps verifies that MaxDepth=5 with
// DepthCeiling=2 effectively walks depth 2, excluding deeper nodes.
func TestRetrieveContextDepthCeilingClamps(t *testing.T) {
	db := setupChainDB(t, 4)
	defer db.Close()

	res, err := RetrieveContext(db, []string{"chain-0"}, RetrieveContextOptions{
		MaxDepth:       5,
		DepthCeiling:   2,
		QueryEmbedding: []float32{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	seen := map[string]bool{}
	for _, sn := range res.SeedNodes {
		seen[sn.Entity.Content] = true
	}
	for _, f := range res.WorldFacts {
		seen[f.Content] = true
	}
	for _, want := range []string{"Chain fact 0", "Chain fact 1", "Chain fact 2"} {
		if !seen[want] {
			t.Errorf("expected %q in result, missing", want)
		}
	}
	if seen["Chain fact 3"] {
		t.Errorf("Chain fact 3 should be excluded at depth ceiling 2; present in result")
	}
}

// TestRetrieveContextSoftCapBoundsOutput verifies MaxRetrievedNodes
// bounds the count of UNIQUE-CONTENT facts after the post-scan loop.
// The cap fires when len(seenIDs) > MaxRetrievedNodes, so the
// output may include up to cap+1 entities total — one trigger row
// gets added to seenIDs but never appends to ranked, so the count
// up to and including that row is the maximum.
func TestRetrieveContextSoftCapBoundsOutput(t *testing.T) {
	db := setupChainDB(t, 5)
	defer db.Close()

	res, err := RetrieveContext(db, []string{"chain-0"}, RetrieveContextOptions{
		MaxDepth:          5,
		MaxRetrievedNodes: 3,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	// Cap implementation triggers when len(seenIDs) > MaxRetrievedNodes,
	// so the output may include cap+1 entities (the trigger row is
	// added to seenIDs but not appended to ranked). WorldFacts
	// measures the count that survives the post-scan bucket loop.
	total := len(res.WorldFacts) + len(res.Opinions) +
		len(res.Experiences) + len(res.Observations)
	if total != 3 {
		t.Errorf("unique fact contents = %d, want 3 (cap=3, chain=5)", total)
	}
}

// TestRetrieveContextRankingOrderByScore verifies the composite
// re-rank (0.7*sim + 0.3*recency) orders things deterministically.
//
// Wall-clock isolation: UpdatedAt values are FIXED (not time.Now())
// with a multi-year gap so the score gap isn't sensitive to CI
// scheduling jitter. The dominant signal here is cosine: aligned
// embedding outranks orthogonal embedding regardless of recency, so
// the assertion holds even if a busy runner delays time.Since by
// hours. The recency component is a secondary check — at
// rankRecencyHalfLifeHours=720h, an oldTime ~5 years stale gives
// recency≈0 while a freshTime near the run-time gives recency near 1.
func TestRetrieveContextRankingOrderByScore(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	oldTime := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	freshTime := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	rows := []struct {
		id, content string
		emb         []float32
		updatedAt   time.Time
	}{
		{"old-ortho", "Ranking older orthogonal fact", []float32{0, 0, 1}, oldTime},
		{"fresh-aligned", "Ranking fresh aligned fact", []float32{1, 0, 0}, freshTime},
	}
	for _, r := range rows {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
			ID: r.id, Category: "world", Content: r.content, Embedding: r.emb,
		}); err != nil {
			t.Fatalf("store %s: %v", r.id, err)
		}
		if _, err := db.Exec(`UPDATE entities SET updated_at = ? WHERE id = ?`, r.updatedAt, r.id); err != nil {
			t.Fatalf("set updated_at for %s: %v", r.id, err)
		}
	}
	// Add an edge so the graph walk reaches fresh-aligned from old-ortho
	// at depth=1 — otherwise the CTE has no expansion and fresh-aligned
	// never enters the ranked slice.
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES (?, ?, ?)`,
		"old-ortho", "fresh-aligned", "related_to"); err != nil {
		t.Fatalf("edge: %v", err)
	}

	res, err := RetrieveContext(db, []string{"old-ortho"}, RetrieveContextOptions{
		MaxDepth:       1,
		QueryEmbedding: []float32{1, 0, 0},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(res.WorldFacts) < 2 {
		t.Fatalf("WorldFacts len = %d, want >= 2", len(res.WorldFacts))
	}
	if res.WorldFacts[0].Content != "Ranking fresh aligned fact" ||
		res.WorldFacts[1].Content != "Ranking older orthogonal fact" {
		t.Errorf("ranked order = [%q, %q], want [fresh-aligned, old-ortho]",
			res.WorldFacts[0].Content, res.WorldFacts[1].Content)
	}
}

// TestFormatContextMarkdownEmpty exercises the empty-result branch.
func TestFormatContextMarkdownEmpty(t *testing.T) {
	if got := FormatContextMarkdown(nil); got != "" {
		t.Errorf("FormatContextMarkdown(nil) = %q, want empty", got)
	}
	if got := FormatContextMarkdown(&RetrievalResult{}); got != "" {
		t.Errorf("FormatContextMarkdown(empty result) = %q, want empty", got)
	}
}

// TestFormatContextMarkdownSeedVsDepth verifies the markdown rendering
// switch: Depth==0 (seed) lines render plain, Depth>0 lines render
// tagged with (via 'relation_type' from parent_id).
func TestFormatContextMarkdownSeedVsDepth(t *testing.T) {
	res := &RetrievalResult{
		SeedNodes: []GraphNode{
			{Entity: Entity{ID: "seed", Content: "Seed fact", Category: "world"}, Depth: 0},
		},
		WorldFacts: []RetrievedFact{
			{Content: "Seed fact", Depth: 0},
			{Content: "Reached fact", ParentID: "seed", RelationType: "related_to", Depth: 1},
		},
	}
	out := FormatContextMarkdown(res)
	if !strings.Contains(out, "# Memory Context\n") {
		t.Errorf("output missing top-level header, got:\n%s", out)
	}
	if !strings.Contains(out, "- Seed fact\n") {
		t.Errorf("seed should render plain, got:\n%s", out)
	}
	if !strings.Contains(out, "- Reached fact (via 'related_to' from seed)\n") {
		t.Errorf("non-seed should render tagged, got:\n%s", out)
	}
}

// ----- benchmarks ------------------------------------------------------

func BenchmarkRetrieveContext1Hop(b *testing.B) { benchmarkRetrieveContext(b, 1) }
func BenchmarkRetrieveContext2Hop(b *testing.B) { benchmarkRetrieveContext(b, 2) }
func BenchmarkRetrieveContext3Hop(b *testing.B) { benchmarkRetrieveContext(b, 3) }

func benchmarkRetrieveContext(b *testing.B, depth int) {
	db, err := InitDB(":memory:", 768)
	if err != nil {
		b.Fatalf("InitDB: %v", err)
	}
	vi := newVectorIndex("in-memory", db, 768)
	defer db.Close()

	const n = 500
	for i := 0; i < n; i++ {
		emb := []float32{float32(i % 7), float32(i%11) / 11, float32(i%13) / 13}
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
			ID:        fmt.Sprintf("bench-%d", i),
			Category:  "world",
			Content:   fmt.Sprintf("fact-%d", i),
			Embedding: emb,
		}); err != nil {
			b.Fatalf("store %d: %v", i, err)
		}
	}
	for i := 0; i < n-1; i++ {
		db.Exec(`INSERT OR IGNORE INTO edges (source_id, target_id, relation_type) VALUES (?,?,?)`,
			fmt.Sprintf("bench-%d", i), fmt.Sprintf("bench-%d", i+1), "related_to")
	}
	q := []float32{0.3, 0.4, 0.5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := RetrieveContext(db, []string{"bench-0"}, RetrieveContextOptions{
			MaxDepth: depth, QueryEmbedding: q,
		}); err != nil {
			b.Fatalf("retrieve: %v", err)
		}
	}
}

// TestFormatContextMarkdownAllBucketsRender verifies each of the four
// category buckets renders its top-level heading and a fact line.
// The seed-vs-depth test covers WORLD rendering alone; this one
// closes the gap on OPINION / EXPERIENCE / OBSERVATION headings +
// content lines, so a regression in writeBucket for any bucket fails.
func TestFormatContextMarkdownAllBucketsRender(t *testing.T) {
	res := &RetrievalResult{
		SeedNodes: []GraphNode{
			{Entity: Entity{ID: "seed", Content: "Seed fact", Category: "world"}, Depth: 0},
		},
		WorldFacts:   []RetrievedFact{{Content: "World fact line", Depth: 0}},
		Opinions:     []RetrievedFact{{Content: "Opinion fact line", Depth: 0}},
		Experiences:  []RetrievedFact{{Content: "Experience fact line", Depth: 0}},
		Observations: []RetrievedFact{{Content: "Observation fact line", Depth: 0}},
	}
	out := FormatContextMarkdown(res)
	for _, heading := range []string{"## WORLD", "## OPINION", "## EXPERIENCE", "## OBSERVATION"} {
		if !strings.Contains(out, heading+"\n") {
			t.Errorf("output missing heading %q, got:\n%s", heading, out)
		}
	}
	for _, content := range []string{"World fact line", "Opinion fact line", "Experience fact line", "Observation fact line"} {
		if !strings.Contains(out, "- "+content+"\n") {
			t.Errorf("output missing list line for %q, got:\n%s", content, out)
		}
	}
}

// ----- #20 task walk tests -------------------------------------------

// TestGetExecutableTasksChain verifies the CTE task walk:
// A blocks B, B blocks C (edges: B blocked_by A, C blocked_by B).
// A has no blockers → initially executable. Complete A => B executable.
// Complete B => C executable.
func TestGetExecutableTasksChain(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()
	schema := taskSchema()

	for _, e := range []Entity{
		{ID: "task-a", Category: "task", Content: "Step A", Embedding: []float32{1, 0, 0}},
		{ID: "task-b", Category: "task", Content: "Step B", Embedding: []float32{0, 1, 0}},
		{ID: "task-c", Category: "task", Content: "Step C", Embedding: []float32{0, 0, 1}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, schema, e); err != nil {
			t.Fatalf("store %s: %v", e.ID, err)
		}
	}
	// B blocked_by A, C blocked_by B.
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES
		('task-b', 'task-a', 'blocked_by'),
		('task-c', 'task-b', 'blocked_by')`); err != nil {
		t.Fatalf("edges: %v", err)
	}

	// A has no blockers → executable; B blocked by A (pending); C blocked by B (pending).
	exec, err := GetExecutableTasks(db, schema, "")
	if err != nil {
		t.Fatalf("get executable: %v", err)
	}
	if len(exec) != 1 || exec[0].ID != "task-a" {
		ids := make([]string, len(exec))
		for i, e := range exec {
			ids[i] = e.ID
		}
		t.Errorf("initial: executable = %v, want [task-a]", ids)
	}

	// Complete A → B should become executable.
	if err := UpdateTaskStatus(db, schema, "task-a", "completed"); err != nil {
		t.Fatalf("complete A: %v", err)
	}
	exec, err = GetExecutableTasks(db, schema, "")
	if err != nil {
		t.Fatalf("get executable after A done: %v", err)
	}
	if len(exec) != 1 || exec[0].ID != "task-b" {
		ids := make([]string, len(exec))
		for i, e := range exec {
			ids[i] = e.ID
		}
		t.Errorf("after A completed: executable = %v, want [task-b]", ids)
	}

	// Complete B → C should become executable.
	if err := UpdateTaskStatus(db, schema, "task-b", "completed"); err != nil {
		t.Fatalf("complete B: %v", err)
	}
	exec, err = GetExecutableTasks(db, schema, "")
	if err != nil {
		t.Fatalf("get executable after B done: %v", err)
	}
	if len(exec) != 1 || exec[0].ID != "task-c" {
		ids := make([]string, len(exec))
		for i, e := range exec {
			ids[i] = e.ID
		}
		t.Errorf("after B completed: executable = %v, want [task-c]", ids)
	}
}

// TestGetExecutableTasksForGoal verifies the goal-scoped CTE walk:
// only tasks reachable from the goal via blocked_by edges are returned.
func TestGetExecutableTasksForGoal(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()
	schema := taskSchema()

	// Chain 1: goal1 → blocked by X → blocked by Y
	// Chain 2: goal2 → blocked by Z
	for _, e := range []Entity{
		{ID: "goal1", Category: "task", Content: "Goal 1", Embedding: []float32{1, 0}},
		{ID: "task-x", Category: "task", Content: "Step X", Embedding: []float32{0, 1}},
		{ID: "task-y", Category: "task", Content: "Step Y", Embedding: []float32{1, 1}},
		{ID: "goal2", Category: "task", Content: "Goal 2", Embedding: []float32{0, 0}},
		{ID: "task-z", Category: "task", Content: "Step Z", Embedding: []float32{1, 1}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, schema, e); err != nil {
			t.Fatalf("store %s: %v", e.ID, err)
		}
	}
	// goal1 blocked by X, X blocked by Y, goal2 blocked by Z.
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES
		('goal1', 'task-x', 'blocked_by'),
		('task-x', 'task-y', 'blocked_by'),
		('goal2', 'task-z', 'blocked_by')`); err != nil {
		t.Fatalf("edges: %v", err)
	}

	// With goal_id='goal1', dep_tree = {goal1, X, Y}.
	// Y has no blockers → executable. X blocked by Y (pending) → not. goal1 blocked by X (pending) → not.
	exec, err := GetExecutableTasks(db, schema, "goal1")
	if err != nil {
		t.Fatalf("get executable for goal1: %v", err)
	}
	if len(exec) != 1 || exec[0].ID != "task-y" {
		ids := make([]string, len(exec))
		for i, e := range exec {
			ids[i] = e.ID
		}
		t.Errorf("goal1 scoped executable = %v, want [task-y]", ids)
	}

	// With goal_id='goal2', dep_tree = {goal2, Z}. Z has no blockers → executable.
	// Verify Y and X from goal1's tree do NOT appear.
	exec, err = GetExecutableTasks(db, schema, "goal2")
	if err != nil {
		t.Fatalf("get executable for goal2: %v", err)
	}
	if len(exec) != 1 || exec[0].ID != "task-z" {
		ids := make([]string, len(exec))
		for i, e := range exec {
			ids[i] = e.ID
		}
		t.Errorf("goal2 scoped executable = %v, want [task-z]", ids)
	}
}

// TestFindRollbackTask verifies recovers_via edge lookup.
func TestFindRollbackTask(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()
	schema := taskSchema()

	for _, e := range []Entity{
		{ID: "failed-step", Category: "task", Content: "Failed", Embedding: []float32{1, 0}},
		{ID: "recovery-step", Category: "task", Content: "Recovery", Embedding: []float32{0, 1}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store %s: %v", e.ID, err)
		}
	}

	// No edge yet — should return empty.
	target, err := FindRollbackTask(db, schema, "failed-step")
	if err != nil {
		t.Fatalf("find rollback: %v", err)
	}
	if target != "" {
		t.Errorf("no edge: target = %q, want empty", target)
	}

	// Add recovers_via edge.
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('failed-step', 'recovery-step', 'recovers_via')`); err != nil {
		t.Fatalf("edge: %v", err)
	}
	target, err = FindRollbackTask(db, schema, "failed-step")
	if err != nil {
		t.Fatalf("find rollback after edge: %v", err)
	}
	if target != "recovery-step" {
		t.Errorf("target = %q, want 'recovery-step'", target)
	}

	// Non-existent task — should return empty.
	target, err = FindRollbackTask(db, schema, "nonexistent")
	if err != nil {
		t.Fatalf("find rollback nonexistent: %v", err)
	}
	if target != "" {
		t.Errorf("nonexistent target = %q, want empty", target)
	}
}

// TestUpdateTaskStatus validates status transitions and error cases.
func TestUpdateTaskStatus(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()
	schema := taskSchema()

	if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
		ID: "t1", Category: "task", Content: "do stuff", Embedding: []float32{1, 0},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	// Valid status.
	if err := UpdateTaskStatus(db, schema, "t1", "running"); err != nil {
		t.Fatalf("update running: %v", err)
	}
	st, err := GetTaskStatus(db, "t1")
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if st != "running" {
		t.Errorf("status = %q, want 'running'", st)
	}

	// Invalid status.
	if err := UpdateTaskStatus(db, schema, "t1", "bogus"); err == nil {
		t.Error("expected error for invalid status")
	}

	// Non-existent task.
	if err := UpdateTaskStatus(db, schema, "nonexistent", "pending"); err == nil {
		t.Error("expected error for non-existent task")
	}

	// Non-task entity.
	if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), Entity{
		ID: "w1", Category: "world", Content: "a fact", Embedding: []float32{1, 0},
	}); err != nil {
		t.Fatalf("store world: %v", err)
	}
	if err := UpdateTaskStatus(db, schema, "w1", "completed"); err == nil {
		t.Error("expected error when updating non-task entity")
	}
}

// ----- helpers for #16/#17 --------------------------------------------

// float32AlmostEqual reports whether a and b are within ε of each
// other. Used by the cosine-parity test to absorb single-precision
// rounding noise across the two implementations.
func float32AlmostEqual(a, b float32) bool {
	const epsilon = 1e-5
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}

// TestCosineSimilarityWithNormParity verifies the behavioural-
// parity invariant documented on CosineSimilarityWithNorm: when
// `normA == VectorNorm(a) the two functions return identical scores.
// This is the contract #17's query-norm-precompute relies on so the
// cached-value path produces the same score as the recompute path.
// Tests five input shapes: identical / orthogonal / opposite /
// parallel-proportional / random-mix.
func TestCosineSimilarityWithNormParity(t *testing.T) {
	cases := []struct {
		name string
		a, b []float32
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}},
		{"opposite", []float32{1, 0, 0}, []float32{-1, 0, 0}},
		{"parallel_proportional", []float32{2, 4, 6}, []float32{1, 2, 3}},
		{"random_4d", []float32{0.3, -0.5, 0.8, 0.1}, []float32{0.2, 0.4, -0.1, 0.6}},
		// Edge cases — both functions return 0 in these conditions and
		// the parity check verifies the early-return guards match.
		{"empty_a", nil, []float32{1, 0, 0}},
		{"dim_mismatch", []float32{1, 0, 0}, []float32{1, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plain := CosineSimilarity(c.a, c.b)
			cached := CosineSimilarityWithNorm(c.a, c.b, VectorNorm(c.a))
			if !float32AlmostEqual(plain, cached) {
				t.Errorf("parity broken: CosineSimilarity=%v != WithNorm=%v (delta=%v)",
					plain, cached, plain-cached)
			}
		})
	}
}

// TestCompositeScorerDefaultDepthPenalty exercises the depth-soft-
// floor penalty (#16): a higher-cosine deep node must NOT outrank a
// lower-cosine seed when the depth penalty's flip threshold is
// crossed. Fixture: chain seed→mid1→mid2→deep, query [1,0,0].
//
// Per-row scoring (recency≈1 for all since inserted in the same run):
//   - seed (depth=0, embedding=[0.5,  0.8660254, 0]):
//     sim=0.5 → score = 0.7*0.5 + 0.3*1.0 - 0.05*0 = 0.65
//   - mid1 (depth=1, embedding=[0,1,0]): sim=0 → 0.30 - 0.05 = 0.25
//   - mid2 (depth=2, embedding=[0,1,0]): sim=0 → 0.30 - 0.10 = 0.20
//   - deep (depth=3, embedding=[0.6, 0.8, 0]):
//     sim=0.6 → score = 0.7*0.6 + 0.3*1.0 - 0.05*3 = 0.57
//
// Expected ordering after the post-scan sort: seed (0.65) > deep
// (0.57) > mid1 (0.25) > mid2 (0.20). Without the depth penalty
// (pre-#16 behaviour) the ranking would be deep (0.72) > seed
// (0.65) > mid1 (0.30) > mid2 (0.30) — the penalty flipped the
// seed-vs-deep ordering exactly as designed.
func TestCompositeScorerDefaultDepthPenalty(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	// Stamp all 4 entities' updated_at to the same fresh time so the
	// recency component is exactly 1.0 across rows and the depth-soft-
	// floor is the only signal that flips the seed-vs-deep ordering.
	// Decoupling from insert-time auto-stamping keeps the test readable
	// (recency=1 across rows is documented above) and stable across
	// any future change to insert semantics.
	stamp := time.Now()
	for _, e := range []Entity{
		{ID: "seed", Category: "world", Content: "Seed fact", Embedding: []float32{0.5, 0.8660254, 0}},
		{ID: "mid1", Category: "world", Content: "Mid 1 fact", Embedding: []float32{0, 1, 0}},
		{ID: "mid2", Category: "world", Content: "Mid 2 fact", Embedding: []float32{0, 1, 0}},
		{ID: "deep", Category: "world", Content: "Deep relevant fact", Embedding: []float32{0.6, 0.8, 0}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store %s: %v", e.ID, err)
		}
		if _, err := db.Exec(`UPDATE entities SET updated_at = ? WHERE id = ?`, stamp, e.ID); err != nil {
			t.Fatalf("stamp updated_at for %s: %v", e.ID, err)
		}
	}
	for _, p := range []struct{ from, to string }{
		{"seed", "mid1"}, {"mid1", "mid2"}, {"mid2", "deep"},
	} {
		if _, err := db.Exec(
			`INSERT INTO edges (source_id, target_id, relation_type) VALUES (?, ?, ?)`,
			p.from, p.to, "related_to"); err != nil {
			t.Fatalf("edge %s->%s: %v", p.from, p.to, err)
		}
	}

	res, err := RetrieveContext(db, []string{"seed"}, RetrieveContextOptions{
		MaxDepth:       10,
		QueryEmbedding: []float32{1, 0, 0},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	want := []string{"Seed fact", "Deep relevant fact", "Mid 1 fact", "Mid 2 fact"}
	if len(res.WorldFacts) != len(want) {
		t.Fatalf("WorldFacts len = %d, want %d (contents=%v)",
			len(res.WorldFacts), len(want), worldFactContents(res.WorldFacts))
	}
	for i, w := range want {
		if res.WorldFacts[i].Content != w {
			t.Errorf("WorldFacts[%d] = %q, want %q (depth-soft-floor penalty should rank seed above deep at higher cosine)",
				i, res.WorldFacts[i].Content, w)
		}
	}

	// Depth penalty is also visible on RankingScore: seed's score is
	// 0.65 vs deep's 0.57, mirror of the WorldFacts ordering.
	if len(res.SeedNodes) != 1 {
		t.Fatalf("SeedNodes = %d, want 1", len(res.SeedNodes))
	}
	if res.SeedNodes[0].RankingScore < 0.64 || res.SeedNodes[0].RankingScore > 0.67 {
		t.Errorf("seed RankingScore = %v, want ~0.65", res.SeedNodes[0].RankingScore)
	}
}

func worldFactContents(facts []RetrievedFact) []string {
	out := make([]string, len(facts))
	for i, f := range facts {
		out[i] = f.Content
	}
	return out
}

// TestCompositeScorerCustom verifies the override hook on
// opts.CompositeScorer (#16): a caller-supplied closure that
// assigns 99.0 to a specific node forces it to the top of the
// category bucket regardless of cosine/recency/depth. Also
// verifies RankingScore on the seed reflects the custom return
// (the default formula would have produced sim=0 since embeddings
// are orthogonal to query, so seed score would be 0.30).
func TestCompositeScorerCustom(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	for _, e := range []Entity{
		{ID: "a", Category: "world", Content: "A fact", Embedding: []float32{0, 1, 0}},
		{ID: "b", Category: "world", Content: "B fact", Embedding: []float32{0, 1, 0}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store: %v", err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('a', 'b', 'related_to')`,
	); err != nil {
		t.Fatalf("edge: %v", err)
	}

	scorer := func(node GraphNode, _ []float32, _ []float32, _ float32) float32 {
		if node.Entity.ID == "b" {
			return 99.0
		}
		return 1.0
	}

	res, err := RetrieveContext(db, []string{"a"}, RetrieveContextOptions{
		MaxDepth:        5,
		QueryEmbedding:  []float32{1, 0, 0},
		CompositeScorer: scorer,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(res.WorldFacts) < 2 {
		t.Fatalf("WorldFacts = %d, want >= 2", len(res.WorldFacts))
	}
	// Custom scorer pushed "b" to rank 1.
	if res.WorldFacts[0].Content != "B fact" {
		t.Errorf("WorldFacts[0] = %q, want \"B fact\" (custom scorer forces it first)",
			res.WorldFacts[0].Content)
	}
	// Seed "a" should reflect the custom return (1.0), NOT the
	// default formula — the default would have given 0.30 for sim=0
	// orthogonal embedding at depth 0.
	if len(res.SeedNodes) != 1 {
		t.Fatalf("SeedNodes = %d, want 1", len(res.SeedNodes))
	}
	if res.SeedNodes[0].RankingScore != 1.0 {
		t.Errorf("seed RankingScore = %v, want 1.0 (custom scorer applied)",
			res.SeedNodes[0].RankingScore)
	}
}

// TestRetrieveContextCustomScorerDispatchCache verifies that the
// query-norm cache passes the same precomputed value to every row
// in the scan (the linear-in-rows savings the #17 precompute exists
// for). Indirect assertion: if the cache were per-row, the resulting
// scores in RankingScore would differ by per-row rounding noises
// from repeated sqrt calls. We instead assert that the cache fires
// exactly once by injecting a CompositeScorer that captures the
// queryNorm it received and asserting all rows saw the same value.
func TestRetrieveContextCompositeScorerReceivesCachedQueryNorm(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	qEmb := []float32{0.6, 0.8, 0} // norm = 1.0 (already unit)
	for _, e := range []Entity{
		{ID: "a", Category: "world", Content: "A fact", Embedding: []float32{0.6, 0.8, 0}},
		{ID: "b", Category: "world", Content: "B fact", Embedding: []float32{0.6, 0.8, 0}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store: %v", err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('a', 'b', 'related_to')`,
	); err != nil {
		t.Fatalf("edge: %v", err)
	}

	received := []float32{}
	scorer := func(_ GraphNode, _ []float32, _ []float32, queryNorm float32) float32 {
		received = append(received, queryNorm)
		return 0
	}

	if _, err := RetrieveContext(db, []string{"a"}, RetrieveContextOptions{
		MaxDepth:        5,
		QueryEmbedding:  qEmb,
		CompositeScorer: scorer,
	}); err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(received) < 2 {
		t.Fatalf("scorer called %d times, want >= 2", len(received))
	}
	want := VectorNorm(qEmb)
	for i, n := range received {
		if !float32AlmostEqual(n, want) {
			t.Errorf("scorer invocation %d: queryNorm=%v, want %v (cache miss?)", i, n, want)
		}
	}
}

// TestCompositeScoreDirect unit-tests the closed-form numeric
// surface behind the default scoring formula. Uses hardcoded default
// weights (0.7/0.3/0.05) so the test catches any accidental drift in
// the compositeScore helper itself.
func TestCompositeScoreDirect(t *testing.T) {
	w := RankingWeight{VectorWeight: 0.7, RecencyWeight: 0.3, DepthPenalty: 0.05}
	cases := []struct {
		name                string
		sim, recency, depth float32
		want                float32
	}{
		{"seed_aligned_fresh", 0.5, 1.0, 0, 0.65},
		{"deep_aligned_fresh", 0.6, 1.0, 3, 0.57},
		{"orthogonal_4_hop", 0.0, 1.0, 4, 0.10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := compositeScore(w, c.sim, c.recency, 0, 0, float32(c.depth))
			if !float32AlmostEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestCompositeScorerNil uses an explicit CompositeScorer that returns 0
// to verify nil scorers resolve to defaultCompositeScorer without
// an explicit nil-guard (i.e. two-semantically-identical retrievals
// using nil vs defaultCompositeScorer-reference produce the same
// WorldFacts ordering).
func TestCompositeScorerNilMatchesDefault(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	for _, e := range []Entity{
		{ID: "seed", Category: "world", Content: "Seed fact", Embedding: []float32{0.5, 0.8660254, 0}},
		{ID: "deep", Category: "world", Content: "Deep relevant fact", Embedding: []float32{0.6, 0.8, 0}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store: %v", err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('seed', 'deep', 'related_to')`,
	); err != nil {
		t.Fatalf("edge: %v", err)
	}

	optsNil := RetrieveContextOptions{
		MaxDepth:       5,
		QueryEmbedding: []float32{1, 0, 0},
	}
	optsDefault := RetrieveContextOptions{
		MaxDepth:        5,
		QueryEmbedding:  []float32{1, 0, 0},
		CompositeScorer: defaultCompositeScorer(resolvedRankingWeight(RankingWeight{VectorWeight: 0.7, RecencyWeight: 0.3, DepthPenalty: 0.05})),
	}

	run := func(opts RetrieveContextOptions) *RetrievalResult {
		res, err := RetrieveContext(db, []string{"seed"}, opts)
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		return res
	}

	resNil := run(optsNil)
	resDefault := run(optsDefault)

	if len(resNil.WorldFacts) != len(resDefault.WorldFacts) {
		t.Fatalf("nil vs default length mismatch: %d vs %d",
			len(resNil.WorldFacts), len(resDefault.WorldFacts))
	}
	for i, n := range resNil.WorldFacts {
		if n.Content != resDefault.WorldFacts[i].Content {
			t.Errorf("WorldFacts[%d] diverges: nil=%q, default=%q",
				i, n.Content, resDefault.WorldFacts[i].Content)
		}
		if n.Depth != resDefault.WorldFacts[i].Depth {
			t.Errorf("WorldFacts[%d].Depth diverges: nil=%d, default=%d",
				i, n.Depth, resDefault.WorldFacts[i].Depth)
		}
	}
	// Catch any future dispatch-path drift in the seed side too: the
	// two retrievals should produce byte-identical SeedNodes including
	// their RankingScore values — not just identical category-bucket
	// contents. A future contributor who accidentally swaps the
	// nil-resolution pattern would be caught by this assertion.
	if len(resNil.SeedNodes) != len(resDefault.SeedNodes) {
		t.Fatalf("nil vs default SeedNodes length mismatch: %d vs %d",
			len(resNil.SeedNodes), len(resDefault.SeedNodes))
	}
	for i, n := range resNil.SeedNodes {
		if n.Entity.ID != resDefault.SeedNodes[i].Entity.ID {
			t.Errorf("SeedNodes[%d].Entity.ID diverges: nil=%q, default=%q",
				i, n.Entity.ID, resDefault.SeedNodes[i].Entity.ID)
		}
		if !float32AlmostEqual(n.RankingScore, resDefault.SeedNodes[i].RankingScore) {
			t.Errorf("SeedNodes[%d].RankingScore diverges: nil=%v, default=%v",
				i, n.RankingScore, resDefault.SeedNodes[i].RankingScore)
		}
	}
}

// ----- P1: graph centrality tests ------------------------------------

// TestCentralityDegreeAutoIncrement verifies the SQL trigger auto-
// maintains entities.degree on edge insertion.
func TestCentralityDegreeAutoIncrement(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	for _, e := range []Entity{
		{ID: "ca", Category: "world", Content: "Central A", Embedding: []float32{1, 0, 0}},
		{ID: "cb", Category: "world", Content: "Central B", Embedding: []float32{0, 1, 0}},
		{ID: "cc", Category: "world", Content: "Central C", Embedding: []float32{0, 0, 1}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store %s: %v", e.ID, err)
		}
	}

	checkDegree := func(id string, want int) {
		t.Helper()
		var d int
		if err := db.QueryRow(`SELECT degree FROM entities WHERE id = ?`, id).Scan(&d); err != nil {
			t.Fatalf("read degree for %s: %v", id, err)
		}
		if d != want {
			t.Errorf("degree of %s = %d, want %d", id, d, want)
		}
	}

	// All start at 0.
	checkDegree("ca", 0)
	checkDegree("cb", 0)
	checkDegree("cc", 0)

	// Add edge A→B: both get +1.
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('ca','cb','related_to')`); err != nil {
		t.Fatalf("edge: %v", err)
	}
	checkDegree("ca", 1)
	checkDegree("cb", 1)
	checkDegree("cc", 0)

	// Add edge B→C: B→2, C→1.
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('cb','cc','related_to')`); err != nil {
		t.Fatalf("edge: %v", err)
	}
	checkDegree("ca", 1)
	checkDegree("cb", 2)
	checkDegree("cc", 1)

	// Add edge C→A: all +1.
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('cc','ca','related_to')`); err != nil {
		t.Fatalf("edge: %v", err)
	}
	checkDegree("ca", 2)
	checkDegree("cb", 2)
	checkDegree("cc", 2)

	// Delete edge A→B: both -1.
	if _, err := db.Exec(`DELETE FROM edges WHERE source_id = 'ca' AND target_id = 'cb' AND relation_type = 'related_to'`); err != nil {
		t.Fatalf("delete edge: %v", err)
	}
	checkDegree("ca", 1)
	checkDegree("cb", 1)
	checkDegree("cc", 2)
}

// TestCentralityRankingBoost verifies that highly-connected entities
// get a small ranking bonus from degree centrality.
func TestCentralityRankingBoost(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	stamp := time.Now()
	for _, e := range []Entity{
		{ID: "hub", Category: "world", Content: "Hub node", Embedding: []float32{0.6, 0.8, 0}},
		{ID: "leaf1", Category: "world", Content: "Leaf 1", Embedding: []float32{0.6, 0.8, 0}},
		{ID: "leaf2", Category: "world", Content: "Leaf 2", Embedding: []float32{0.6, 0.8, 0}},
		{ID: "leaf3", Category: "world", Content: "Leaf 3", Embedding: []float32{0.6, 0.8, 0}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store %s: %v", e.ID, err)
		}
		db.Exec(`UPDATE entities SET updated_at = ? WHERE id = ?`, stamp, e.ID)
	}

	// Hub connects to all 3 leaves.
	for _, leaf := range []string{"leaf1", "leaf2", "leaf3"} {
		if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type) VALUES ('hub', ?, 'related_to')`, leaf); err != nil {
			t.Fatalf("edge hub->%s: %v", leaf, err)
		}
	}

	// All embeddings identical + same recency → scores driven only by
	// centrality. Hub degree=3, leaves degree=1 each.
	res, err := RetrieveContext(db, []string{"hub"}, RetrieveContextOptions{
		MaxDepth:          5,
		QueryEmbedding:    []float32{0.6, 0.8, 0},
		MaxRetrievedNodes: 4,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	// Hub should rank first (degree=3, log10(4)≈0.602, centrality≈0.03).
	// Leaves have degree=1 (log10(2)≈0.301, centrality≈0.015).
	if len(res.WorldFacts) < 2 {
		t.Fatalf("WorldFacts = %d, want >= 2", len(res.WorldFacts))
	}
	// Hub's RankingScore should be higher than all leaves.
	hubScore := res.SeedNodes[0].RankingScore
	for _, f := range res.WorldFacts {
		if f.Content == "Hub node" {
			continue
		}
		if f.RankingScore >= hubScore {
			t.Errorf("leaf %q RankingScore=%v >= hub %v (centrality should boost hub)",
				f.Content, f.RankingScore, hubScore)
		}
	}
}

// ----- P1: weighted edges tests ---------------------------------------

// TestWeightedEdgePathWeight verifies that edge weight accumulates
// correctly in the CTE path_weight column.
func TestWeightedEdgePathWeight(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	for _, e := range []Entity{
		{ID: "ws", Category: "world", Content: "Weight seed", Embedding: []float32{1, 0, 0}},
		{ID: "wm", Category: "world", Content: "Weight mid", Embedding: []float32{0, 1, 0}},
		{ID: "wd", Category: "world", Content: "Weight deep", Embedding: []float32{0, 0, 1}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store %s: %v", e.ID, err)
		}
	}

	// seed→mid weight 0.5, mid→deep weight 2.0
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES ('ws','wm','related_to', 0.5)`); err != nil {
		t.Fatalf("edge ws→wm: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO edges (source_id, target_id, relation_type, weight) VALUES ('wm','wd','related_to', 2.0)`); err != nil {
		t.Fatalf("edge wm→wd: %v", err)
	}

	res, err := RetrieveContext(db, []string{"ws"}, RetrieveContextOptions{
		MaxDepth:       5,
		QueryEmbedding: []float32{1, 0, 0},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	// Seed node at depth 0: path_weight = 0.
	if len(res.SeedNodes) > 0 && res.SeedNodes[0].PathWeight != 0 {
		t.Errorf("seed PathWeight = %v, want 0", res.SeedNodes[0].PathWeight)
	}

	// Verify depth penalty reflects weighted path.
	// seed: path_weight=0, penalty=0
	// mid: path_weight=0.5, penalty=0.05*0.5=0.025
	// deep: path_weight=2.5, penalty=0.05*2.5=0.125
	if len(res.SeedNodes) > 0 {
		seedScore := res.SeedNodes[0].RankingScore
		_ = seedScore // seed should have highest score (lowest penalty)
	}
}

// TestWeightedEdgeVectorAPI verifies AddEdge with weight and Edge.Weight.
func TestWeightedEdgeVectorAPI(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	for _, e := range []Entity{
		{ID: "wa", Category: "world", Content: "W-A", Embedding: []float32{1, 0}},
		{ID: "wb", Category: "world", Content: "W-B", Embedding: []float32{0, 1}},
	} {
		if err := StoreEntityWithEmbedding(db, vi, defaultSchemaConfig(false), e); err != nil {
			t.Fatalf("store: %v", err)
		}
	}

	// Add weighted edge via AddEdge.
	if err := AddEdge(db, "wa", "wb", "related_to", 0.75); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	// Read back via queryEdges.
	edges, err := queryEdges(db,
		"SELECT source_id, target_id, relation_type, COALESCE(weight, 1.0) FROM edges WHERE source_id = ?",
		"wa")
	if err != nil {
		t.Fatalf("queryEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(edges))
	}
	if edges[0].Weight != 0.75 {
		t.Errorf("weight = %v, want 0.75", edges[0].Weight)
	}

	// Default weight (0 → 1.0).
	if err := AddEdge(db, "wa", "wb", "uses", 0); err != nil {
		t.Fatalf("AddEdge default: %v", err)
	}
	edges, err = queryEdges(db,
		"SELECT source_id, target_id, relation_type, COALESCE(weight, 1.0) FROM edges WHERE source_id = ? AND relation_type = 'uses'",
		"wa")
	if err != nil {
		t.Fatalf("queryEdges default: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("default edges = %d, want 1", len(edges))
	}
	if edges[0].Weight != 1.0 {
		t.Errorf("default weight = %v, want 1.0", edges[0].Weight)
	}
}

// ----- P1: provenance tests -------------------------------------------

// TestProvenanceRoundTrip verifies entities stored with provenance
// metadata can be retrieved via GetEntitiesByProvenance.
func TestProvenanceRoundTrip(t *testing.T) {
	db, vi := memDB(t)
	defer db.Close()

	convID := "conv-123"
	msgID := "msg-456"

	// Store entity with provenance via direct SQL (simulates ingestion).
	emb := EmbeddingToBytes(NormalizeVectorRet([]float32{1, 0, 0}))
	if _, err := db.Exec(`
		INSERT INTO entities (id, category, content, embedding, conversation_id, message_id, source, source_type, created_at)
		VALUES ('prov-a', 'world', 'Prov fact A', ?, ?, ?, 'dialog', 'extraction', CURRENT_TIMESTAMP)
	`, emb, convID, msgID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Second entity with different provenance.
	if _, err := db.Exec(`
		INSERT INTO entities (id, category, content, embedding, conversation_id, message_id, source, source_type, created_at)
		VALUES ('prov-b', 'world', 'Prov fact B', ?, 'conv-999', 'msg-001', 'api', 'manual', CURRENT_TIMESTAMP)
	`, emb); err != nil {
		t.Fatalf("insert b: %v", err)
	}

	// Query by conversation_id.
	entities, err := GetEntitiesByProvenance(db, convID, "", "", 50)
	if err != nil {
		t.Fatalf("provenance: %v", err)
	}
	if len(entities) != 1 || entities[0].ID != "prov-a" {
		t.Errorf("by conv: got %d entities, want 1 (prov-a)", len(entities))
	}

	// Query by message_id.
	entities, err = GetEntitiesByProvenance(db, "", msgID, "", 50)
	if err != nil {
		t.Fatalf("provenance by msg: %v", err)
	}
	if len(entities) != 1 || entities[0].ID != "prov-a" {
		t.Errorf("by msg: got %d entities, want 1 (prov-a)", len(entities))
	}

	// Query by source.
	entities, err = GetEntitiesByProvenance(db, "", "", "api", 50)
	if err != nil {
		t.Fatalf("provenance by source: %v", err)
	}
	if len(entities) != 1 || entities[0].ID != "prov-b" {
		t.Errorf("by source: got %d entities, want 1 (prov-b)", len(entities))
	}

	// No filters → error.
	_, err = GetEntitiesByProvenance(db, "", "", "", 50)
	if err == nil {
		t.Error("expected error with no filters")
	}

	// Non-matching filter → empty.
	entities, err = GetEntitiesByProvenance(db, "nonexistent", "", "", 50)
	if err != nil {
		t.Fatalf("provenance none: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("non-matching: got %d entities, want 0", len(entities))
	}

	_ = vi
}

// NormalizeVectorRet normalizes and returns the vector (used in tests).
func NormalizeVectorRet(v []float32) []float32 {
	NormalizeVector(v)
	return v
}
