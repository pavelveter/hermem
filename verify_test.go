package main

import (
	"fmt"
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
	db, err := InitDB("timing-test.db")
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Create test data
	for i := 0; i < 1000; i++ {
		entity := Entity{
			ID:       fmt.Sprintf("timing-fact-%d", i),
			Category: "world",
			Content:  fmt.Sprintf("Test fact number %d", i),
			Embedding: []float32{
				float32(i) / 1000.0,
				float32(i%100) / 100.0,
				float32(i%10) / 10.0,
			},
		}
		if err := StoreEntityWithEmbedding(db, entity); err != nil {
			t.Fatalf("Failed to store entity: %v", err)
		}
	}

	// Test retrieval timing
	queryEmbedding := []float32{0.5, 0.5, 0.5}
	
	start := time.Now()
	_, err = SearchByVector(db, queryEmbedding, 10)
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}
	searchDuration := time.Since(start)

	start = time.Now()
	_, err = RetrieveContext(db, []string{"timing-fact-0"}, RetrieveContextOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("Failed to retrieve context: %v", err)
	}
	retrievalDuration := time.Since(start)

	fmt.Printf("Vector search: %v\n", searchDuration)
	fmt.Printf("Retrieval: %v\n", retrievalDuration)

	if searchDuration > 5*time.Millisecond {
		t.Errorf("Vector search took longer than 5ms (%v)", searchDuration)
	}

	if retrievalDuration > 5*time.Millisecond {
		t.Errorf("Retrieval took longer than 5ms (%v)", retrievalDuration)
	}
}
