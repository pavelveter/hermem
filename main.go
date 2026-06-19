package main

import (
	"database/sql"
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

func GenerateResponse(db *sql.DB, embedder Embedder, userQuery string) (string, error) {
	queryEmbedding, err := embedder.Embed(userQuery)
	if err != nil {
		return "", fmt.Errorf("failed to embed query: %w", err)
	}

	searchResults, err := SearchByVector(db, queryEmbedding, 3)
	if err != nil {
		return "", fmt.Errorf("failed to search: %w", err)
	}

	var seedIDs []string
	for _, res := range searchResults {
		seedIDs = append(seedIDs, res.Entity.ID)
	}

	contextResult, err := RetrieveContext(db, seedIDs, 2)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve context: %w", err)
	}

	return FormatContextMarkdown(contextResult), nil
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

	fmt.Println("Dialog ingested successfully")

	userQuery := "Tell me about France"
	context, err := GenerateResponse(db, embedder, userQuery)
	if err != nil {
		log.Fatalf("Failed to generate response: %v", err)
	}

	fmt.Printf("\n=== Context for: %q ===\n", userQuery)
	if context == "" {
		fmt.Println("(no context found)")
	} else {
		fmt.Println(context)
	}
}
