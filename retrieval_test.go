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
	db := memDB(t)
	for i := 0; i < n; i++ {
		emb := []float32{float32(i) + 1, float32(i) + 2, float32(i) + 3}
		if err := StoreEntityWithEmbedding(db, Entity{
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
	db := memDB(t)
	defer db.Close()

	for _, e := range []Entity{
		{ID: "A", Category: "world", Content: "node A", Embedding: []float32{1, 0}},
		{ID: "B", Category: "world", Content: "node B", Embedding: []float32{0, 1}},
	} {
		if err := StoreEntityWithEmbedding(db, e); err != nil {
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
// "fresh-aligned" should outrank "old-ortho" because both have
// query-aligned cosine (1.0 vs ~0) and freshness difference (now vs
// 720h ago → recency 1.0 vs ~0.37).
func TestRetrieveContextRankingOrderByScore(t *testing.T) {
	db := memDB(t)
	defer db.Close()

	now := time.Now()
	rows := []struct {
		id, content string
		emb         []float32
		updatedAt   time.Time
	}{
		{"old-ortho", "Ranking older orthogonal fact", []float32{0, 0, 1}, now.Add(-720 * time.Hour)},
		{"fresh-aligned", "Ranking fresh aligned fact", []float32{1, 0, 0}, now},
	}
	for _, r := range rows {
		if err := StoreEntityWithEmbedding(db, Entity{
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
	defer db.Close()

	const n = 500
	for i := 0; i < n; i++ {
		emb := []float32{float32(i % 7), float32(i%11) / 11, float32(i%13) / 13}
		if err := StoreEntityWithEmbedding(db, Entity{
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
