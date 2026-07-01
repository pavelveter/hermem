// Example: Hermem Go SDK usage.
//
// Prerequisites:
//   - Running Hermem server: hermem serve
//   - API key (if configured): export HERMEM_API_KEY=your-key
//
// Run: go run ./sdk/go/examples/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	hermem "github.com/pavelveter/hermem/sdk/go"
)

func main() {
	baseURL := envOrDefault("HERMEM_URL", "http://localhost:8420")
	apiKey := os.Getenv("HERMEM_API_KEY")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	opts := []hermem.Option{hermem.WithTimeout(10 * time.Second)}
	if apiKey != "" {
		opts = append(opts, hermem.WithAPIKey(apiKey))
	}
	client := hermem.New(baseURL, opts...)

	// --- Memory ---
	fmt.Println("=== Memory ===")

	err := client.Memory.Store(ctx, &hermem.StoreRequest{
		ID:       "example-1",
		Category: "fact",
		Content:  "The Hermem knowledge graph supports semantic search and multi-hop retrieval.",
	})
	if err != nil {
		log.Printf("store: %v", err)
	} else {
		fmt.Println("Store: ok")
	}

	searchResult, err := client.Memory.Search(ctx, &hermem.SearchRequest{
		Query: "semantic search",
		TopK:  5,
	})
	if err != nil {
		log.Printf("search: %v", err)
	} else {
		fmt.Printf("Search: %d results\n", len(searchResult))
	}

	// --- Tasks ---
	fmt.Println("\n=== Tasks ===")

	task, err := client.Task.Create(ctx, &hermem.TaskCreateRequest{
		Content:    "Implement MCP server integration",
		ContextIDs: []string{"example-1"},
	})
	if err != nil {
		log.Printf("create task: %v", err)
	} else {
		fmt.Printf("Task created: %s\n", task.ID)
	}

	tasks, err := client.Task.List(ctx, &hermem.TaskListRequest{Status: "pending"})
	if err != nil {
		log.Printf("list tasks: %v", err)
	} else {
		fmt.Printf("Pending tasks: %d\n", len(tasks.Tasks))
	}

	// --- Graph ---
	fmt.Println("\n=== Graph ===")

	components, err := client.Graph.ConnectedComponents(ctx, 2)
	if err != nil {
		log.Printf("components: %v", err)
	} else {
		fmt.Printf("Components: %d\n", len(components))
	}

	// --- Admin ---
	fmt.Println("\n=== Admin ===")

	health, err := client.Admin.Health(ctx)
	if err != nil {
		log.Printf("health: %v", err)
	} else {
		fmt.Printf("Health: %s\n", health.Status)
	}

	fmt.Println("\nDone!")
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
