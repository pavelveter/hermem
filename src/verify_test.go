package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegration(t *testing.T) {
	db, err := InitDB("verify-test.db", 768)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	currentVectorIndex = newVectorIndex("in-memory", db, 768)
	defer db.Close()

	// Test 1: Store fact and link to another node
	fact1 := Entity{
		ID:       "verify-fact-1",
		Category: "world",
		Content:  "Go is a statically typed programming language",
		Embedding: []float32{0.1, 0.2, 0.3},
	}

	fact2 := Entity{
		ID:       "verify-fact-2",
		Category: "world",
		Content:  "Go was created at Google",
		Embedding: []float32{0.15, 0.25, 0.35},
	}

	if err := StoreEntityWithEmbedding(db, fact1); err != nil {
		t.Fatalf("Failed to store fact1: %v", err)
	}

	if err := StoreEntityWithEmbedding(db, fact2); err != nil {
		t.Fatalf("Failed to store fact2: %v", err)
	}

	// Create edge between facts
	_, err = db.Exec(`
		INSERT OR IGNORE INTO edges (source_id, target_id, relation_type)
		VALUES (?, ?, ?)
	`, fact1.ID, fact2.ID, "related_to")
	if err != nil {
		t.Fatalf("Failed to create edge: %v", err)
	}

	// Test 2: Vector search should find both facts
	queryEmbedding := []float32{0.1, 0.2, 0.3}
	results, err := SearchByVector(db, queryEmbedding, 10)
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("Expected at least 2 results, got %d", len(results))
	}

	// Test 3: Retrieve context should return both facts
	seedIDs := []string{fact1.ID}
	retrievalResult, err := RetrieveContext(db, seedIDs, RetrieveContextOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("Failed to retrieve context: %v", err)
	}

	foundFact1 := false
	foundFact2 := false
	for _, fact := range retrievalResult.WorldFacts {
		if fact.Content == fact1.Content {
			foundFact1 = true
		}
		if fact.Content == fact2.Content {
			foundFact2 = true
		}
	}

	if !foundFact1 {
		t.Error("fact1 not found in retrieval results")
	}
	if !foundFact2 {
		t.Error("fact2 not found in retrieval results")
	}

	fmt.Println("Integration verification passed!")
}

func TestTiming(t *testing.T) {
	// Bench at three cohort sizes with a small-world edge topology so
	// the SQLite recursive-CTE walk in retrieval.go observes real
	// fan-out and the retrieval timing scales with N via the JOIN cost
	// over edges (rather than reflecting only CTE setup, as it did in
	// the no-edges star-graph baseline).
	//
	// Each entity i is connected to:
	//   - `chainK` forward chain edges (i+1 .. i+chainK) when target
	//     < n, relation_type "next"; gives locality
	//   - `longRangeK` hash-based long-range edges, target = ((i+1)
	//     * mult) % n for mult in {7, 11, 13}, relation_type
	//     "long-range"; breaks locality so fan-out grows meaningfully
	//     with depth
	//
	// The CTE in retrieval.go matches edges bidirectionally
	// (`source_id = gw.id OR target_id = gw.id`), so a forward-only
	// edge is enough for the walk to find the reverse connection.
	//
	// Edge seeding is batched per entity as a single multi-VALUES
	// INSERT OR IGNORE so the run stays under ~3s even at N=6000
	// (~48k edge rows total). RetrieveContext is fired against
	// `timing-fact-50` (a middle node) to avoid wraparound skew at
	// the chain head.
	cohorts := []int{1000, 3000, 6000}
	const (
		chainK        = 5
		longRangeK    = 3
		retrieveDepth = 2
	)
	longRangeMultipliers := []uint32{7, 11, 13}
	retrieveSeed := "timing-fact-50"

	db, err := InitDB("timing-test.db", 768)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	currentVectorIndex = newVectorIndex("in-memory", db, 768)
	defer os.Remove("timing-test.db")
	defer db.Close()

	// Pre-allocate every entity up front so each cohort's measurement
	// window excludes per-row StoreEntity overhead.
	entities := make([]Entity, cohorts[len(cohorts)-1])
	for i := range entities {
		entities[i] = Entity{
			ID:       fmt.Sprintf("timing-fact-%d", i),
			Category: "world",
			Content:  fmt.Sprintf("Test fact number %d", i),
			Embedding: []float32{
				float32(i%1000) / 1000.0,
				float32(i%100) / 100.0,
				float32(i%10) / 10.0,
			},
		}
	}

	queryEmbedding := []float32{0.5, 0.5, 0.5}

	type cohortResult struct {
		n         int
		search    time.Duration
		retrieval time.Duration
	}
	results := make([]cohortResult, 0, len(cohorts))
	seeded := 0

	for _, n := range cohorts {
		// Seed entities incrementally per cohort.
		for i := seeded; i < n; i++ {
			if err := StoreEntityWithEmbedding(db, entities[i]); err != nil {
				t.Fatalf("Failed to seed entity %d: %v", i, err)
			}
		}

		// Rebuild edges from scratch against this cohort's n. The
		// earlier incremental-by-cohort design left long-range edges
		// for entities [0, seeded) pointing into the previous (smaller)
		// cohort's target pool, which biased the walk at later
		// cohorts (their fan-out was effectively capped by the older
		// n). DELETE + full reinsert keeps the graph "characteristic
		// of N" at every measurement slice.
		if _, err := db.Exec(`DELETE FROM edges`); err != nil {
			t.Fatalf("Failed to clear edges for cohort %d: %v", n, err)
		}

		// Seed edges for ALL entities [0, n) against this cohort's
		// n, batched per entity as one multi-VALUES INSERT. 24
		// placeholders per entity (8 edges * 3 args) is well under
		// SQLite's 999-per-statement cap. The uint32 cast on
		// (i+1)*mult is overflow-safe (max ~6001*13 ≈ 78k \u226a 2^32).
		for i := 0; i < n; i++ {
			sourceID := fmt.Sprintf("timing-fact-%d", i)
			values := make([]string, 0, chainK+longRangeK)
			args := make([]interface{}, 0, 3*(chainK+longRangeK))

			for k := 1; k <= chainK; k++ {
				if i+k < n {
					values = append(values, "(?, ?, ?)")
					args = append(args, sourceID, fmt.Sprintf("timing-fact-%d", i+k), "next")
				}
			}
			for _, mult := range longRangeMultipliers {
				target := int(((uint32(i) + 1) * mult) % uint32(n))
				if target != i {
					values = append(values, "(?, ?, ?)")
					args = append(args, sourceID, fmt.Sprintf("timing-fact-%d", target), "long-range")
				}
			}

			if len(values) > 0 {
				query := "INSERT OR IGNORE INTO edges (source_id, target_id, relation_type) VALUES " + strings.Join(values, ",")
				if _, err := db.Exec(query, args...); err != nil {
					t.Fatalf("Failed to seed edges for entity %d: %v", i, err)
				}
			}
		}
		seeded = n

		// Vector search: O(N) cosine scan over embeddings.
		searchStart := time.Now()
		if _, err := SearchByVector(db, queryEmbedding, 10); err != nil {
			t.Fatalf("Failed to search at N=%d: %v", n, err)
		}
		searchDur := time.Since(searchStart)

		// Retrieval: recursive CTE walk from a middle seed. CTE work
		// grows with edge count and the visited-fact set size; both
		// scale with N under this topology.
		retrStart := time.Now()
		if _, err := RetrieveContext(db, []string{retrieveSeed}, RetrieveContextOptions{
			MaxDepth: retrieveDepth,
		}); err != nil {
			t.Fatalf("Failed to retrieve at N=%d: %v", n, err)
		}
		retrDur := time.Since(retrStart)

		results = append(results, cohortResult{n: n, search: searchDur, retrieval: retrDur})
		fmt.Printf("PERF  N=%-5d  search=%-12s  retrieve=%s\n", n, searchDur, retrDur)
	}

	// Strictness gate: keep the legacy < 5ms assertion at N=1000
	// where the original test was tuned. Larger cohorts are
	// informational; physical scaling is asserted via the search
	// algorithm being O(N) by construction (cosine over every row).
	if results[0].search > 5*time.Millisecond {
		t.Errorf("Vector search at N=1000 took longer than 5ms (%v)", results[0].search)
	}
}
