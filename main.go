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

	// Test data
	entities := []Entity{
		{ID: "paris", Category: "world", Content: "Paris is the capital of France", Embedding: []float32{0.1, 0.2, 0.3}},
		{ID: "france", Category: "world", Content: "France is a country in Europe", Embedding: []float32{0.2, 0.3, 0.4}},
		{ID: "europe", Category: "world", Content: "Europe is a continent", Embedding: []float32{0.3, 0.4, 0.5}},
		{ID: "paris-opinion", Category: "opinion", Content: "Paris is beautiful in spring", Embedding: []float32{0.15, 0.25, 0.35}},
		{ID: "france-exp", Category: "experience", Content: "I visited Paris last summer", Embedding: []float32{0.25, 0.35, 0.45}},
		{ID: "paris-obs", Category: "observation", Content: "Paris has many museums", Embedding: []float32{0.12, 0.22, 0.32}},
	}

	for _, e := range entities {
		if err := StoreEntityWithEmbedding(db, e); err != nil {
			log.Fatalf("Failed to store entity %s: %v", e.ID, err)
		}
	}

	// Create edges
	edges := []Edge{
		{SourceID: "paris", TargetID: "france", RelationType: "located_in"},
		{SourceID: "france", TargetID: "europe", RelationType: "located_in"},
		{SourceID: "paris-opinion", TargetID: "paris", RelationType: "about"},
		{SourceID: "france-exp", TargetID: "france", RelationType: "about"},
		{SourceID: "paris-obs", TargetID: "paris", RelationType: "about"},
	}

	for _, edge := range edges {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO edges (source_id, target_id, relation_type)
			VALUES (?, ?, ?)
		`, edge.SourceID, edge.TargetID, edge.RelationType)
		if err != nil {
			log.Fatalf("Failed to create edge: %v", err)
		}
	}

	// Test vector search
	queryEmbedding := []float32{0.1, 0.2, 0.3}
	searchResults, err := SearchByVector(db, queryEmbedding, 3)
	if err != nil {
		log.Fatalf("Failed to search: %v", err)
	}

	fmt.Println("=== Vector Search Results ===")
	for _, r := range searchResults {
		fmt.Printf("ID: %s, Similarity: %.3f, Content: %s\n", r.Entity.ID, r.Similarity, r.Entity.Content)
	}

	// Test retrieval pipeline
	seedIDs := make([]string, len(searchResults))
	for i, r := range searchResults {
		seedIDs[i] = r.Entity.ID
	}

	retrievalResult, err := RetrieveContext(db, seedIDs, 2)
	if err != nil {
		log.Fatalf("Failed to retrieve context: %v", err)
	}

	fmt.Println("\n=== Retrieved Context ===")
	fmt.Println(FormatContextMarkdown(retrievalResult))
}
