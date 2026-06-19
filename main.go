package main

import (
	"fmt"
	"log"
)

func main() {
	db, err := InitDB("hermem.db")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Test embedding storage and search
	testEntity := Entity{
		ID:       "test-1",
		Category: "world",
		Content:  "The capital of France is Paris",
		Embedding: []float32{0.1, 0.2, 0.3, 0.4, 0.5},
	}

	if err := StoreEntityWithEmbedding(db, testEntity); err != nil {
		log.Fatalf("Failed to store entity: %v", err)
	}

	queryEmbedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	results, err := SearchByVector(db, queryEmbedding, 5)
	if err != nil {
		log.Fatalf("Failed to search: %v", err)
	}

	fmt.Printf("Found %d results\n", len(results))
	for _, r := range results {
		fmt.Printf("ID: %s, Similarity: %.3f, Content: %s\n", r.Entity.ID, r.Similarity, r.Entity.Content)
	}
}
