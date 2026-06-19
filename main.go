package main

import (
	"fmt"
	"hash/fnv"
	"log"
)

type MockEmbedder struct{}

func (m *MockEmbedder) Embed(text string) ([]float32, error) {
	h := fnv.New32a()
	h.Write([]byte(text))
	hash := h.Sum32()

	return []float32{
		float32(hash&0xFF) / 255.0,
		float32((hash>>8)&0xFF) / 255.0,
		float32((hash>>16)&0xFF) / 255.0,
	}, nil
}

func main() {
	cfg, err := LoadConfig("hermem.ini")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	db, err := InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	embedder := cfg.NewEmbedder()
	extractor := &SimpleLLMExtractor{}
	worker := NewIngestionWorker(db, extractor, embedder)

	dialog := `User: What is the capital of France?
Assistant: The capital of France is Paris.
User: Tell me about Paris
Assistant: Paris is a beautiful city with many museums.`

	if err := worker.ProcessDialog(dialog); err != nil {
		log.Fatalf("Failed to process dialog: %v", err)
	}

	fmt.Println("Dialog processed successfully")

	queryEmbedding := []float32{0.1, 0.2, 0.3}
	searchResults, err := SearchByVector(db, queryEmbedding, 5)
	if err != nil {
		log.Fatalf("Failed to search: %v", err)
	}

	fmt.Println("\n=== Vector Search Results After Ingestion ===")
	for _, r := range searchResults {
		fmt.Printf("ID: %s, Similarity: %.3f, Content: %s\n", r.Entity.ID, r.Similarity, r.Entity.Content)
	}
}
