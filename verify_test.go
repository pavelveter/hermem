package main

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestIntegration(t *testing.T) {
	db, err := InitDB("verify-test.db")
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
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
	// Bench at three cohort sizes so the README ## Performance table
	// shows how the in-memory cosine scales with entity count. The
	// smallest cohort keeps a strict < 5ms regression gate; the
	// larger cohorts are informational only (the README consumes them
	// verbatim via the `PERF  N=…` lines below).
	cohorts := []int{1000, 3000, 6000}

	db, err := InitDB("timing-test.db")
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer os.Remove("timing-test.db")
	defer db.Close()

	// Pre-allocate every entity up front so each cohort's measurement
	// window excludes the per-row StoreEntity overhead, which would
	// otherwise dominate at every cohort boundary and skew the
	// reported timings.
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
		for i := seeded; i < n; i++ {
			if err := StoreEntityWithEmbedding(db, entities[i]); err != nil {
				t.Fatalf("Failed to seed entity %d: %v", i, err)
			}
		}
		seeded = n

		searchStart := time.Now()
		if _, err := SearchByVector(db, queryEmbedding, 10); err != nil {
			t.Fatalf("Failed to search at N=%d: %v", n, err)
		}
		searchDur := time.Since(searchStart)

		retrStart := time.Now()
		if _, err := RetrieveContext(db, []string{"timing-fact-0"}, RetrieveContextOptions{MaxDepth: 2}); err != nil {
			t.Fatalf("Failed to retrieve at N=%d: %v", n, err)
		}
		retrDur := time.Since(retrStart)

		results = append(results, cohortResult{n: n, search: searchDur, retrieval: retrDur})
		fmt.Printf("PERF  N=%-5d  search=%-12s  retrieve=%s\n", n, searchDur, retrDur)
	}

	// Strictness gate: keep the legacy < 5ms assertion at N=1000
	// where the original test was tuned. Larger cohorts are reported
	// but not asserted; physical scaling is asserted via the search
	// algorithm being O(N) by construction (cosine over every row).
	if results[0].search > 5*time.Millisecond {
		t.Errorf("Vector search at N=1000 took longer than 5ms (%v)", results[0].search)
	}
	if results[0].retrieval > 5*time.Millisecond {
		t.Errorf("Retrieval at N=1000 took longer than 5ms (%v)", results[0].retrieval)
	}
}
