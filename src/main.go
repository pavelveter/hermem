package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func GenerateResponse(ctx context.Context, db *sql.DB, vi VectorIndex, embedder Embedder, opts RetrieveContextOptions, userQuery string) (string, error) {
	queryEmbedding, err := embedder.Embed(ctx, userQuery)
	if err != nil {
		return "", fmt.Errorf("failed to embed query: %w", err)
	}

	searchResults, err := SearchByVector(db, vi, queryEmbedding, 3)
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
	opts.Ctx = ctx
	contextResult, err := RetrieveContext(db, seedIDs, opts)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve context: %w", err)
	}

	return FormatContextMarkdown(contextResult), nil
}

func readInput() string {
	stat, err := os.Stdin.Stat()
	if err != nil {
		log.Fatalf("Failed to stat stdin: %v", err)
	}
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		log.Fatal("this command expects JSON piped into stdin (not a terminal)")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Failed to read stdin: %v", err)
	}
	return strings.TrimSpace(string(data))
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: hermem <command> [args]\n\nCommands:\n  store    Store a fact (JSON on stdin)\n  search   Search memory (JSON on stdin)\n  query    Full pipeline: search + graph walk (JSON on stdin)\n  edge     Add an edge (JSON on stdin)\n  ingest   Ingest dialog (JSON on stdin)\n  serve    Run HTTP server\n")
		os.Exit(1)
	}

	cfg, err := LoadConfigFromBinaryDir()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	db, err := InitDB(resolveDBPath(cfg.DBPath), cfg.VectorDim)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	vi := newVectorIndex(cfg.VectorBackend, db, cfg.VectorDim)
	metricsWorker = InitMetricsWorker(db)
	defer metricsWorker.Stop()

	embedder := cfg.NewEmbedder()
	extractor := cfg.NewExtractor()

	cmd := os.Args[1]
	ctx := context.Background()

	switch cmd {
	case "store":
		var req struct {
			ID        string    `json:"id"`
			Category  string    `json:"category"`
			Content   string    `json:"content"`
			Embedding []float32 `json:"embedding,omitempty"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.ID == "" || req.Category == "" || req.Content == "" {
			log.Fatal("id, category, content required")
		}
		entity := Entity{ID: req.ID, Category: req.Category, Content: req.Content, Embedding: req.Embedding}
		if len(entity.Embedding) == 0 {
			embedding, err := embedder.Embed(ctx, entity.Content)
			if err != nil {
				log.Fatalf("Failed to embed: %v", err)
			}
			entity.Embedding = embedding
		}
		if err := StoreEntityWithEmbedding(db, vi, entity); err != nil {
			log.Fatalf("Failed to store: %v", err)
		}
		if err := AutoLinkEdges(ctx, db, vi, embedder, entity.ID, entity.Embedding); err != nil {
			log.Fatalf("Failed to auto-link: %v", err)
		}
		fmt.Println(`{"status":"ok"}`)

	case "search":
		var req struct {
			Query string `json:"query"`
			TopK  int    `json:"top_k"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.Query == "" {
			log.Fatal("query required")
		}
		if req.TopK <= 0 {
			req.TopK = 5
		}
		embedding, err := embedder.Embed(ctx, req.Query)
		if err != nil {
			log.Fatalf("Embed failed: %v", err)
		}
		results, err := SearchByVector(db, vi, embedding, req.TopK)
		if err != nil {
			log.Fatalf("Search failed: %v", err)
		}
		json.NewEncoder(os.Stdout).Encode(results)

	case "query":
		var req struct {
			Query string `json:"query"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.Query == "" {
			log.Fatal("query required")
		}
		opts := RetrieveContextOptions{
			MaxDepth:          2,
			DepthCeiling:      cfg.MaxDepthCeiling,
			MaxRetrievedNodes: cfg.MaxRetrievedNodes,
		}
		context, err := GenerateResponse(ctx, db, vi, embedder, opts, req.Query)
		if err != nil {
			log.Fatalf("Query failed: %v", err)
		}
		json.NewEncoder(os.Stdout).Encode(map[string]string{"context": context})

	case "edge":
		var req struct {
			SourceID     string `json:"source_id"`
			TargetID     string `json:"target_id"`
			RelationType string `json:"relation_type"`
			AutoCreate   bool   `json:"auto_create"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
			log.Fatal("source_id, target_id, relation_type required")
		}
		if !validRelationTypes[req.RelationType] {
			log.Fatalf("invalid relation_type: %s", req.RelationType)
		}
		if req.AutoCreate {
			if err := AddEdgeWithAutoCreate(ctx, db, vi, embedder, req.SourceID, req.TargetID, req.RelationType); err != nil {
				log.Fatalf("Failed to add edge: %v", err)
			}
		} else {
			if err := AddEdge(db, req.SourceID, req.TargetID, req.RelationType); err != nil {
				log.Fatalf("Failed to add edge: %v", err)
			}
		}
		fmt.Println(`{"status":"ok"}`)

	case "ingest":
		var req struct {
			Dialog string `json:"dialog"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.Dialog == "" {
			log.Fatal("dialog required")
		}
		worker := NewIngestionWorker(db, vi, extractor, embedder, cfg.DedupThreshold)
		if err := worker.ProcessDialog(ctx, req.Dialog); err != nil {
			log.Fatalf("Ingest failed: %v", err)
		}
		fmt.Println(`{"status":"ok"}`)

	case "serve":
		port := "8420"
		if len(os.Args) > 2 {
			port = os.Args[2]
		}
		srv := NewServer(db, vi, embedder, extractor, cfg.DedupThreshold, RetrieveContextOptions{
			DepthCeiling:      cfg.MaxDepthCeiling,
			MaxRetrievedNodes: cfg.MaxRetrievedNodes,
		})

		gcCtx, gcCancel := context.WithCancel(ctx)
		defer gcCancel()
		go GarbageCollector(gcCtx, db, vi, cfg.Retention)

		mux := http.NewServeMux()
		mux.HandleFunc("/health", srv.HandleHealth)
		mux.HandleFunc("/metrics", metricsHandler)
		mux.HandleFunc("/store", srv.HandleStore)
		mux.HandleFunc("/search", srv.HandleSearch)
		mux.HandleFunc("/retrieve", srv.HandleRetrieve)
		mux.HandleFunc("/ingest", srv.HandleIngest)
		mux.HandleFunc("/query", srv.HandleQuery)
		mux.HandleFunc("/edge", srv.HandleEdge)

		middlewareStack := recoveryMiddleware(requestIDMiddleware(authMiddleware(cfg.APIKey)(slogMiddleware(mux))))

		httpServer := &http.Server{
			Addr:         ":" + port,
			Handler:      middlewareStack,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 120 * time.Second,
			IdleTimeout:  120 * time.Second,
		}

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			slog.Info("server ready",
				"event", "server_ready",
				"port", port,
			)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server error: %v", err)
			}
		}()

		<-quit
		slog.Info("shutting down...", "event", "server_shutdown")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("server forced shutdown: %v", err)
		}
		slog.Info("server stopped", "event", "server_stopped")

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}
