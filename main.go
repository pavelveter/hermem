package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

func GenerateResponse(db *sql.DB, embedder Embedder, opts RetrieveContextOptions, userQuery string) (string, error) {
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

	// Reuse the same embedding for the re-rank so the score reflects
	// similarity to exactly the question that drove the seed selection.
	// Safe mutation: opts is the value-type copy owned by GenerateResponse,
	// not the caller's struct.
	opts.QueryEmbedding = queryEmbedding
	contextResult, err := RetrieveContext(db, seedIDs, opts)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve context: %w", err)
	}

	return FormatContextMarkdown(contextResult), nil
}

func readInput() string {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Failed to read stdin: %v", err)
	}
	return strings.TrimSpace(string(data))
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: hermem <command> [args]\n\nCommands:\n  store    Store a fact (JSON on stdin)\n  search   Search memory (JSON on stdin)\n  query    Full pipeline: search + graph walk (JSON on stdin)\n  ingest   Ingest dialog (JSON on stdin)\n  serve    Run HTTP server\n")
		os.Exit(1)
	}

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
	extractor := cfg.NewExtractor()

	cmd := os.Args[1]

	switch cmd {
	case "store":
		var req struct {
			ID       string    `json:"id"`
			Category string    `json:"category"`
			Content  string    `json:"content"`
			Embedding []float32 `json:"embedding,omitempty"`
		}
		if err := json.Unmarshal([]byte(readInput()), &req); err != nil {
			log.Fatalf("Invalid JSON: %v", err)
		}
		if req.ID == "" || req.Category == "" || req.Content == "" {
			log.Fatal("id, category, content required")
		}
		entity := Entity{ID: req.ID, Category: req.Category, Content: req.Content, Embedding: req.Embedding}
		if len(entity.Embedding) == 0 {
			embedding, err := embedder.Embed(entity.Content)
			if err != nil {
				log.Fatalf("Failed to embed: %v", err)
			}
			entity.Embedding = embedding
		}
		if err := StoreEntityWithEmbedding(db, entity); err != nil {
			log.Fatalf("Failed to store: %v", err)
		}
		fmt.Println(`{"status":"ok"}`)

	case "search":
		var req struct {
			Query string `json:"query"`
			TopK  int    `json:"top_k"`
		}
		if err := json.Unmarshal([]byte(readInput()), &req); err != nil {
			log.Fatalf("Invalid JSON: %v", err)
		}
		if req.Query == "" {
			log.Fatal("query required")
		}
		if req.TopK <= 0 {
			req.TopK = 5
		}
		embedding, err := embedder.Embed(req.Query)
		if err != nil {
			log.Fatalf("Embed failed: %v", err)
		}
		results, err := SearchByVector(db, embedding, req.TopK)
		if err != nil {
			log.Fatalf("Search failed: %v", err)
		}
		json.NewEncoder(os.Stdout).Encode(results)

	case "query":
		var req struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal([]byte(readInput()), &req); err != nil {
			log.Fatalf("Invalid JSON: %v", err)
		}
		if req.Query == "" {
			log.Fatal("query required")
		}
		opts := RetrieveContextOptions{
			MaxDepth:          2,
			DepthCeiling:      cfg.MaxDepthCeiling,
			MaxRetrievedNodes: cfg.MaxRetrievedNodes,
		}
		context, err := GenerateResponse(db, embedder, opts, req.Query)
		if err != nil {
			log.Fatalf("Query failed: %v", err)
		}
		json.NewEncoder(os.Stdout).Encode(map[string]string{"context": context})

	case "ingest":
		var req struct {
			Dialog string `json:"dialog"`
		}
		if err := json.Unmarshal([]byte(readInput()), &req); err != nil {
			log.Fatalf("Invalid JSON: %v", err)
		}
		if req.Dialog == "" {
			log.Fatal("dialog required")
		}
		worker := NewIngestionWorker(db, extractor, embedder, cfg.DedupThreshold)
		if err := worker.ProcessDialog(req.Dialog); err != nil {
			log.Fatalf("Ingest failed: %v", err)
		}
		fmt.Println(`{"status":"ok"}`)

	case "serve":
		port := "8420"
		if len(os.Args) > 2 {
			port = os.Args[2]
		}
		srv := NewServer(db, embedder, extractor, cfg.DedupThreshold, RetrieveContextOptions{
			DepthCeiling:      cfg.MaxDepthCeiling,
			MaxRetrievedNodes: cfg.MaxRetrievedNodes,
		})
		http.HandleFunc("/health", srv.HandleHealth)
		http.HandleFunc("/store", srv.HandleStore)
		http.HandleFunc("/search", srv.HandleSearch)
		http.HandleFunc("/retrieve", srv.HandleRetrieve)
		http.HandleFunc("/ingest", srv.HandleIngest)
		http.HandleFunc("/query", srv.HandleQuery)
		slog.Info("hermem server listening",
			"event", "server_ready",
			"port", port,
		)
		log.Fatal(http.ListenAndServe(":"+port, nil))

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}
