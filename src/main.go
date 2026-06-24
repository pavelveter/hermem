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
	opts.QueryText = userQuery
	opts.Ctx = ctx
	contextResult, err := MultiHopRetrieveContext(db, vi, embedder, seedIDs, opts)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve context: %w", err)
	}

	return FormatContextMarkdown(contextResult), nil
}

func truncateSlice(s []string, max int) []string {
	if len(s) <= max {
		return s
	}
	return append(s[:max], "...")
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
	// --help / -h short-circuits before any DB work so a typo
	// doesn't open a SQLite handle just to print the command list.
	for _, a := range os.Args[1:] {
		if a == "--help" || a == "-h" {
			printUsage(os.Stdout)
			os.Exit(0)
		}
	}

	if len(os.Args) < 2 {
		printUsage(os.Stderr)
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

	InitMetricsDB(db)

	vi := newVectorIndex(cfg.VectorBackend, db, cfg.VectorDim)
	metricsWorker = InitMetricsWorker(db)
	defer metricsWorker.Stop()

	embedder := cfg.NewEmbedder()
	extractor := cfg.NewExtractor()
	reranker := cfg.NewReranker()

	// Sprint 1: Runtime struct is defined in runtime.go for future
	// multi-tenant/library use. For now, per-command access goes
	// through local db/vi/cfg.Schema references.

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
		if err := cfg.ValidateCategory(req.Category); err != nil {
			log.Fatalf("invalid request: %v", err)
		}
		entity := Entity{ID: req.ID, Category: req.Category, Content: req.Content, Embedding: req.Embedding}
		if len(entity.Embedding) == 0 {
			embedding, err := embedder.Embed(ctx, entity.Content)
			if err != nil {
				log.Fatalf("Failed to embed: %v", err)
			}
			entity.Embedding = embedding
		}
		if err := StoreEntityWithEmbedding(db, vi, cfg.Schema, entity); err != nil {
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
			RankingWeight:     cfg.Ranking,
			Reranker:          reranker,
		}
		context, err := GenerateResponse(ctx, db, vi, embedder, opts, req.Query)
		if err != nil {
			log.Fatalf("Query failed: %v", err)
		}
		json.NewEncoder(os.Stdout).Encode(map[string]string{"context": context})

	case "edge":
		var req struct {
			SourceID     string  `json:"source_id"`
			TargetID     string  `json:"target_id"`
			RelationType string  `json:"relation_type"`
			AutoCreate   bool    `json:"auto_create"`
			Weight       float32 `json:"weight,omitempty"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
			log.Fatal("source_id, target_id, relation_type required")
		}
		if err := cfg.ValidateRelation(req.RelationType); err != nil {
			log.Fatalf("invalid request: %v", err)
		}
		if req.AutoCreate {
			if err := AddEdgeWithAutoCreate(ctx, db, vi, embedder, req.SourceID, req.TargetID, req.RelationType); err != nil {
				log.Fatalf("Failed to add edge: %v", err)
			}
		} else {
			if err := AddEdge(db, req.SourceID, req.TargetID, req.RelationType, req.Weight); err != nil {
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
		worker := NewIngestionWorker(db, vi, extractor, embedder, cfg.DedupThreshold, cfg.Schema)
		if err := worker.ProcessDialog(ctx, req.Dialog); err != nil {
			log.Fatalf("Ingest failed: %v", err)
		}
		fmt.Println(`{"status":"ok"}`)

	case "task-status":
		var req struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.ID == "" || req.Status == "" {
			log.Fatal("id, status required")
		}
		if err := UpdateTaskStatus(db, cfg.Schema, req.ID, req.Status); err != nil {
			log.Fatalf("task status update failed: %v", err)
		}
		fmt.Println(`{"status":"ok"}`)

	case "task-executable":
		data := readInput()
		if strings.TrimSpace(data) == "" {
			data = "{}"
		}
		var req struct {
			GoalID string `json:"goal_id"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(data)), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		tasks, err := GetExecutableTasks(db, cfg.Schema, req.GoalID)
		if err != nil {
			log.Fatalf("failed to get executable tasks: %v", err)
		}
		if tasks == nil {
			tasks = []Entity{}
		}
		json.NewEncoder(os.Stdout).Encode(TaskExecutableResponse{Tasks: tasks})

	case "task-next":
		data := readInput()
		if strings.TrimSpace(data) == "" {
			data = "{}"
		}
		var req struct {
			GoalID string `json:"goal_id"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(data)), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		tasks, err := GetExecutableTasks(db, cfg.Schema, req.GoalID)
		if err != nil {
			log.Fatalf("failed to get next tasks: %v", err)
		}
		if tasks == nil {
			tasks = []Entity{}
		}
		json.NewEncoder(os.Stdout).Encode(TaskExecutableResponse{Tasks: tasks})

	case "task-list":
		var req struct {
			Status string `json:"status"`
			GoalID string `json:"goal_id"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		tasks, err := ListTasks(db, cfg.Schema, req.Status, req.GoalID)
		if err != nil {
			log.Fatalf("failed to list tasks: %v", err)
		}
		if tasks == nil {
			tasks = []Entity{}
		}
		json.NewEncoder(os.Stdout).Encode(TaskExecutableResponse{Tasks: tasks})

	case "task-show":
		var req struct {
			ID string `json:"id"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.ID == "" {
			log.Fatal("id required")
		}
		entity, blocked, recovers, err := GetTaskWithRelations(db, cfg.Schema, req.ID)
		if err != nil {
			log.Fatalf("task show failed: %v", err)
		}
		json.NewEncoder(os.Stdout).Encode(TaskShowResponse{Entity: entity, BlockedBy: blocked, RecoversVia: recovers})

	case "task-dep":
		var req struct {
			SourceID     string `json:"source_id"`
			TargetID     string `json:"target_id"`
			RelationType string `json:"relation_type"`
			Add          bool   `json:"add"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.SourceID == "" || req.TargetID == "" {
			log.Fatal("source_id, target_id required")
		}
		rel := req.RelationType
		if rel == "" {
			rel = cfg.Schema.RelationBlocking
		}
		if err := cfg.ValidateRelation(rel); err != nil {
			log.Fatalf("invalid request: %v", err)
		}
		if req.Add {
			if err := AddEdge(db, req.SourceID, req.TargetID, rel, 1.0); err != nil {
				log.Fatalf("failed to add dependency: %v", err)
			}
		} else {
			if err := DeleteEdge(db, req.SourceID, req.TargetID, rel); err != nil {
				log.Fatalf("failed to remove dependency: %v", err)
			}
		}
		fmt.Println(`{"status":"ok"}`)

	case "task-tree":
		var req struct {
			GoalID string `json:"goal_id"`
			ID     string `json:"id,omitempty"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.ID != "" {
			req.GoalID = req.ID
		}
		nodes, err := GetTaskTree(db, cfg.Schema, req.GoalID)
		if err != nil {
			log.Fatalf("failed to get task tree: %v", err)
		}
		fmt.Print(RenderTaskTree(nodes, ""))

	case "task-create":
		var req struct {
			ID         string   `json:"id"`
			Content    string   `json:"content"`
			ContextIDs []string `json:"context_ids,omitempty"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.Content == "" {
			log.Fatal("content required")
		}
		if req.ID == "" {
			req.ID = fmt.Sprintf("task-%d", time.Now().UnixNano())
		}
		embedding, err := embedder.Embed(ctx, req.Content)
		if err != nil {
			log.Fatalf("Failed to embed: %v", err)
		}
		category := firstStatefulCategory(cfg.Schema)
		if category == "" {
			log.Fatal("no stateful category configured")
		}
		entity := Entity{ID: req.ID, Category: category, Content: req.Content, Embedding: embedding}
		if err := StoreEntityWithEmbedding(db, vi, cfg.Schema, entity); err != nil {
			log.Fatalf("Failed to store: %v", err)
		}
		for _, cid := range req.ContextIDs {
			if cid == "" {
				continue
			}
			if err := AddEdge(db, req.ID, cid, "related_to", 1.0); err != nil {
				slog.Error("failed to add context edge", "err", err, "from", req.ID, "to", cid)
			}
		}
		if err := AutoLinkEdges(ctx, db, vi, embedder, req.ID, embedding); err != nil {
			log.Fatalf("Failed to auto-link: %v", err)
		}
		fmt.Println(`{"status":"ok"}`)

	case "temporal":
		// Phase 10: temporal retrieval — query with time range.
		var req struct {
			Query    string `json:"query"`
			TimeFrom string `json:"time_from"`
			TimeTo   string `json:"time_to"`
			TopK     int    `json:"top_k"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.Query == "" {
			log.Fatal("query required")
		}
		if req.TopK <= 0 {
			req.TopK = 3
		}
		queryEmbedding, err := embedder.Embed(ctx, req.Query)
		if err != nil {
			log.Fatalf("Embed failed: %v", err)
		}
		searchResults, err := SearchByVector(db, vi, queryEmbedding, req.TopK)
		if err != nil {
			log.Fatalf("Search failed: %v", err)
		}
		var seedIDs []string
		for _, res := range searchResults {
			seedIDs = append(seedIDs, res.Entity.ID)
		}
		opts := RetrieveContextOptions{
			MaxDepth:          2,
			DepthCeiling:      cfg.MaxDepthCeiling,
			MaxRetrievedNodes: cfg.MaxRetrievedNodes,
			QueryEmbedding:    queryEmbedding,
			QueryText:         req.Query,
			RankingWeight:     cfg.Ranking,
			Reranker:          reranker,
		}
		if req.TimeFrom != "" {
			if t, err := time.Parse(time.RFC3339, req.TimeFrom); err == nil {
				opts.TimeFrom = t
			}
		}
		if req.TimeTo != "" {
			if t, err := time.Parse(time.RFC3339, req.TimeTo); err == nil {
				opts.TimeTo = t
			}
		}
		result, err := RetrieveContext(db, seedIDs, opts)
		if err != nil {
			log.Fatalf("Temporal retrieval failed: %v", err)
		}
		json.NewEncoder(os.Stdout).Encode(result)

	case "timeline":
		// Phase 10: episodic memory — timeline of recent entities.
		limit := 50
		if len(os.Args) > 2 {
			if n, err := fmt.Sscanf(os.Args[2], "%d", &limit); err != nil || n != 1 || limit <= 0 {
				limit = 50
			}
		}
		rows, err := db.QueryContext(ctx, `
			SELECT id, category, content, created_at,
			       source, source_type, conversation_id, message_id
			FROM entities
			WHERE archived = 0 AND created_at IS NOT NULL
			ORDER BY created_at DESC
			LIMIT ?
		`, limit)
		if err != nil {
			log.Fatalf("timeline: %v", err)
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var id, category, content string
			var createdAt sql.NullTime
			var source, sourceType, convID, msgID sql.NullString
			if err := rows.Scan(&id, &category, &content, &createdAt,
				&source, &sourceType, &convID, &msgID); err != nil {
				log.Fatalf("scan timeline: %v", err)
			}
			ts := "(unknown)"
			if createdAt.Valid {
				ts = createdAt.Time.Format(time.RFC3339)
			}
			info := ""
			if convID.Valid && convID.String != "" {
				info += fmt.Sprintf(" conv=%s", convID.String)
			}
			fmt.Printf("[%s] %s  %s  [%s]%s\n", ts, id, content, category, info)
			count++
		}
		if count == 0 {
			fmt.Println("No timeline entries found.")
		}

	case "contradictions":
		// Phase 10: contradiction graph — list contradicts edges.
		// Optional entity ID from argv[2] to filter by entity.
		entityID := ""
		if len(os.Args) > 2 {
			entityID = os.Args[2]
		}
		pairs, err := GetContradictions(db, entityID)
		if err != nil {
			log.Fatalf("contradictions: %v", err)
		}
		if len(pairs) == 0 {
			fmt.Println("No contradictions found.")
		} else {
			for _, p := range pairs {
				fmt.Printf("[%s] %s\n  contradicts [%s] %s\n\n", p.SourceID, p.SourceContent, p.TargetID, p.TargetContent)
			}
		}

	case "explain":
		// Sprint 2: retrieval explainability — full pipeline with
		// vector/recency/depth breakdown per fact.
		var req struct {
			Query string `json:"query"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.Query == "" {
			log.Fatal("query required")
		}
		queryEmbedding, err := embedder.Embed(ctx, req.Query)
		if err != nil {
			log.Fatalf("Embed failed: %v", err)
		}
		searchResults, err := SearchByVector(db, vi, queryEmbedding, 3)
		if err != nil {
			log.Fatalf("Search failed: %v", err)
		}
		var seedIDs []string
		for _, res := range searchResults {
			seedIDs = append(seedIDs, res.Entity.ID)
		}
		opts := RetrieveContextOptions{
			MaxDepth:          2,
			DepthCeiling:      cfg.MaxDepthCeiling,
			MaxRetrievedNodes: cfg.MaxRetrievedNodes,
			QueryEmbedding:    queryEmbedding,
			QueryText:         req.Query,
			Explain:           true,
			RankingWeight:     cfg.Ranking,
			Reranker:          reranker,
		}
		result, err := RetrieveContext(db, seedIDs, opts)
		if err != nil {
			log.Fatalf("Explain failed: %v", err)
		}
		json.NewEncoder(os.Stdout).Encode(result)

	case "task-rollback":
		var req struct {
			ID string `json:"id"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.ID == "" {
			log.Fatal("id required")
		}
		rollbackID, err := FindRollbackTask(db, cfg.Schema, req.ID)
		if err != nil {
			log.Fatalf("failed to find rollback task: %v", err)
		}
		json.NewEncoder(os.Stdout).Encode(TaskRollbackResponse{RollbackTaskID: rollbackID})

	case "agent-loop":
		// Phase 10: agent state engine — execution loop for a goal.
		// JSON stdin: {goal_id}.
		var req struct {
			GoalID string `json:"goal_id"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.GoalID == "" {
			log.Fatal("goal_id required")
		}
		slog.Info("agent loop started", "event", "agent_loop_start", "goal_id", req.GoalID)
		err := AgentLoop(ctx, db, cfg.Schema, req.GoalID, func(ctx context.Context, task Entity) error {
			fmt.Printf("[%s] %s  [%s]\n", task.ID, task.Content, task.Category)
			return nil
		})
		if err != nil {
			log.Fatalf("agent loop: %v", err)
		}

	case "verify":
		// Sprint 1: graph integrity verifier — read-only sanity checks.
		report, err := VerifyGraph(db, cfg.Schema, cfg.VectorDim)
		if err != nil {
			log.Fatalf("verify failed: %v", err)
		}
		fmt.Print(report.String())
		if !report.Pass() {
			os.Exit(1)
		}

	case "migrate":
		// Sprint 4: versioned migration system — show status and
		// apply pending migrations. Safe to call on a fresh DB
		// (runMigrations is idempotent).
		status, err := MigrationStatus(db)
		if err != nil {
			log.Fatalf("migration status: %v", err)
		}
		pending := 0
		for _, m := range status {
			mark := "  "
			if m.Applied {
				mark = "OK"
			} else {
				mark = "--"
				pending++
			}
			fmt.Printf("[%s] %s", mark, m.Name)
			if m.AppliedAt != "" {
				fmt.Printf("  (%s)", m.AppliedAt)
			}
			fmt.Println()
		}
		if pending > 0 {
			fmt.Printf("\n%d pending migration(s) — run 'hermem migrate' again after InitDB has applied them.\n", pending)
		}

	case "migration-rollback":
		// P0: rollback the last applied migration.
		name, err := RollbackMigration(db)
		if err != nil {
			log.Fatalf("rollback: %v", err)
		}
		if name == "" {
			fmt.Println("No migrations to roll back.")
		} else {
			fmt.Printf("Rolled back: %s\n", name)
		}

	case "migration-verify":
		// P0: verify checksums of all applied migrations.
		mismatches, err := VerifyMigrationIntegrity(db)
		if err != nil {
			log.Fatalf("verify: %v", err)
		}
		if len(mismatches) == 0 {
			fmt.Println("All migration checksums intact.")
		} else {
			fmt.Printf("%d mismatch(es):\n", len(mismatches))
			for _, m := range mismatches {
				fmt.Printf("  %s: stored=%s current=%s\n", m.Name, m.StoredChecksum, m.CurrentChecksum)
			}
			os.Exit(1)
		}

	case "execution-plan":
		// P1: show topologically sorted task execution plan for a goal.
		var req struct {
			GoalID string `json:"goal_id"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.GoalID == "" {
			log.Fatal("goal_id required")
		}
		tasks, err := ExecutionPlan(db, cfg.Schema, req.GoalID)
		if err != nil {
			log.Fatalf("execution plan: %v", err)
		}
		for _, t := range tasks {
			fmt.Printf("[%s] %s  [%s]\n", t.ID, t.Content, t.Status)
		}

	case "recovery-plan":
		// P2: recovery plan — walk recovers_via chain from a failed task.
		var req struct {
			ID string `json:"id"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if req.ID == "" {
			log.Fatal("id required")
		}
		plan, err := GenerateRecoveryPlan(db, cfg.Schema, req.ID)
		if err != nil {
			log.Fatalf("recovery plan: %v", err)
		}
		if len(plan) == 0 {
			fmt.Println("No recovery plan found (no recovers_via edges configured).")
		} else {
			for i, t := range plan {
				fmt.Printf("%d. [%s] %s  [%s]\n", i+1, t.ID, t.Content, t.Status)
			}
		}

	case "connected-components":
		// P2: graph clustering — find connected components.
		minSize := 2
		if len(os.Args) > 2 {
			fmt.Sscanf(os.Args[2], "%d", &minSize)
		}
		components, err := FindConnectedComponents(db, minSize)
		if err != nil {
			log.Fatalf("connected components: %v", err)
		}
		if len(components) == 0 {
			fmt.Println("No connected components found.")
		} else {
			for _, c := range components {
				fmt.Printf("Component (size=%d, avg_degree=%.1f): %v\n",
					c.Size, c.AvgDegree, truncateSlice(c.IDs, 5))
			}
		}

	case "provenance":
		// P1: provenance API — query entities by conversation, message, source.
		conversationID := ""
		messageID := ""
		sourceFilter := ""
		limit := 50
		args := os.Args[2:]
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--conversation":
				if i+1 < len(args) {
					conversationID = args[i+1]
					i++
				}
			case "--message":
				if i+1 < len(args) {
					messageID = args[i+1]
					i++
				}
			case "--source":
				if i+1 < len(args) {
					sourceFilter = args[i+1]
					i++
				}
			case "--limit":
				if i+1 < len(args) {
					if n, err := fmt.Sscanf(args[i+1], "%d", &limit); err != nil || n != 1 {
						limit = 50
					}
					i++
				}
			}
		}
		entities, err := GetEntitiesByProvenance(db, conversationID, messageID, sourceFilter, limit)
		if err != nil {
			log.Fatalf("provenance: %v", err)
		}
		for _, e := range entities {
			ts := ""
			if e.CreatedAt != nil {
				ts = e.CreatedAt.Format(time.RFC3339)
			}
			fmt.Printf("[%s] %s  [%s]  %s  conv=%s msg=%s\n",
				ts, e.ID, e.Category, truncate(e.Content, 80),
				e.ConversationID, e.MessageID)
		}
		if len(entities) == 0 {
			fmt.Println("No entities found for given provenance filters.")
		}

	case "communities":
		// P2: community detection — Louvain modularity optimisation.
		maxIter := 50
		minSize := 2
		args := os.Args[2:]
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--max-iterations":
				if i+1 < len(args) {
					fmt.Sscanf(args[i+1], "%d", &maxIter)
					i++
				}
			case "--min-size":
				if i+1 < len(args) {
					fmt.Sscanf(args[i+1], "%d", &minSize)
					i++
				}
			}
		}
		communities, globalQ, err := DetectCommunities(db, maxIter)
		if err != nil {
			log.Fatalf("community detection: %v", err)
		}
		fmt.Printf("Global modularity: %.6f\n", globalQ)
		shown := 0
		for _, c := range communities {
			if c.Size < minSize {
				continue
			}
			fmt.Printf("\n[%s] size=%d modularity=%.6f\n", c.ID, c.Size, c.Modularity)
			for _, m := range c.Members {
				fmt.Printf("  - %s\n", m)
			}
			shown++
		}
		if shown == 0 {
			fmt.Println("No communities found above the minimum size threshold.")
		}

	case "re-embed":
		// P2: background re-embedding — update all entity embeddings
		// after model/dim change.
		batchSize := 50
		model := ""
		args := os.Args[2:]
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--batch-size":
				if i+1 < len(args) {
					fmt.Sscanf(args[i+1], "%d", &batchSize)
					i++
				}
			case "--model":
				if i+1 < len(args) {
					model = args[i+1]
					i++
				}
			}
		}
		needs, oldDim, err := NeedsReEmbed(db, cfg.VectorDim)
		if err != nil {
			log.Fatalf("re-embed check: %v", err)
		}
		if needs {
			fmt.Printf("Dimension drift detected: DB has dim=%d, config has dim=%d\n", oldDim, cfg.VectorDim)
			fmt.Println("Starting re-embedding...")
		} else {
			fmt.Printf("No dimension drift (dim=%d). Re-embedding anyway...\n", cfg.VectorDim)
		}
		result, err := ReEmbedAll(ctx, db, vi, embedder, cfg.VectorDim, batchSize, model)
		if err != nil {
			log.Fatalf("re-embed: %v", err)
		}
		fmt.Printf("Re-embed complete: %d/%d entities (failed=%d, batches=%d, elapsed=%s)\n",
			result.ReEmbedded, result.TotalEntities, result.Failed, result.Batches, result.Elapsed)

	case "quantize":
		// P2: vector quantization — compress/decompress an embedding.
		var req struct {
			Embedding []float32 `json:"embedding"`
		}
		if _, _, msg, ok := decodeStrict(bytes.NewReader([]byte(readInput())), &req); !ok {
			log.Fatalf("invalid request: %s", msg)
		}
		if len(req.Embedding) == 0 {
			log.Fatal("embedding required")
		}
		qv := QuantizeVector(req.Embedding)
		deq := DequantizeVector(qv)
		rawBytes := len(req.Embedding) * 4
		qBytes := 8 + len(qv.Codes)
		fmt.Printf("Original: %d elements (%d bytes)\n", len(req.Embedding), rawBytes)
		fmt.Printf("Quantized: %d bytes (%.1fx compression)\n", qBytes, float64(rawBytes)/float64(qBytes))
		fmt.Printf("Min=%.4f Max=%.4f\n", qv.Min, qv.Max)
		// Compute max reconstruction error.
		maxErr := float32(0)
		for i := range req.Embedding {
			e := req.Embedding[i] - deq[i]
			if e < 0 {
				e = -e
			}
			if e > maxErr {
				maxErr = e
			}
		}
		fmt.Printf("Max reconstruction error: %.6f\n", maxErr)

	case "schema":
		// Sprint 4: show current vs stored schema fingerprint.
		stored, current, err := CheckSchemaFingerprint(db, cfg.Schema)
		if err != nil {
			log.Fatalf("schema fingerprint: %v", err)
		}
		fmt.Printf("Current schema fingerprint:  %s\n", current)
		if stored != "" {
			fmt.Printf("Stored schema fingerprint:   %s\n", stored)
			if stored != current {
				fmt.Println("WARNING: schema has changed since last stored fingerprint.")
				fmt.Println("Run 'hermem migrate' to apply any pending migrations.")
				fmt.Println("Send SIGHUP to the serve process to reload schema at runtime.")
			}
		} else {
			fmt.Println("Stored schema fingerprint:   (none — fresh database)")
		}

	case "serve":
		port := "8420"
		if len(os.Args) > 2 {
			port = os.Args[2]
		}

		// Phase 6: OpenTelemetry tracing setup
		traceShutdown, err := InitTracing()
		if err != nil {
			slog.Warn("tracing init failed, continuing without traces", "event", "tracing_init_error", "error", err)
		} else {
			defer traceShutdown()
		}

		srv := NewServer(db, vi, embedder, extractor, cfg.DedupThreshold, RetrieveContextOptions{
			DepthCeiling:      cfg.MaxDepthCeiling,
			MaxRetrievedNodes: cfg.MaxRetrievedNodes,
			RankingWeight:     cfg.Ranking,
			Reranker:          reranker,
		}, cfg.Schema)

		gcCtx, gcCancel := context.WithCancel(ctx)
		gcDone := make(chan struct{})
		go func() {
			GarbageCollector(gcCtx, db, vi, cfg.Retention)
			close(gcDone)
		}()

		mux := http.NewServeMux()
		mux.HandleFunc("/health", srv.HandleHealth)
		mux.HandleFunc("/health/live", srv.HandleHealthLive)
		mux.HandleFunc("/health/ready", srv.HandleHealthReady)
		mux.HandleFunc("/metrics", metricsHandler)
		mux.HandleFunc("/store", srv.HandleStore)
		mux.HandleFunc("/search", srv.HandleSearch)
		mux.HandleFunc("/retrieve", srv.HandleRetrieve)
		mux.HandleFunc("/ingest", srv.HandleIngest)
		mux.HandleFunc("/query", srv.HandleQuery)
		mux.HandleFunc("/edge", srv.HandleEdge)
		mux.HandleFunc("/task/status", srv.HandleTaskStatus)
		mux.HandleFunc("/task/executable", srv.HandleTaskExecutable)
		mux.HandleFunc("/task/next", srv.HandleTaskExecutable)
		mux.HandleFunc("/task/list", srv.HandleTaskList)
		mux.HandleFunc("/task/show", srv.HandleTaskShow)
		mux.HandleFunc("/task/dep", srv.HandleTaskDep)
		mux.HandleFunc("/task/tree", srv.HandleTaskTree)
		mux.HandleFunc("/task/create", srv.HandleTaskCreate)
		mux.HandleFunc("/task/rollback", srv.HandleTaskRollback)
		mux.HandleFunc("/query/explain", srv.HandleQueryExplain)
		mux.HandleFunc("/contradictions", srv.HandleContradictions)
		mux.HandleFunc("/query/temporal", srv.HandleQueryTemporal)
		mux.HandleFunc("/timeline", srv.HandleTimeline)
		mux.HandleFunc("/provenance", srv.HandleProvenance)
		mux.HandleFunc("/recovery-plan", srv.HandleRecoveryPlan)
		mux.HandleFunc("/connected-components", srv.HandleConnectedComponents)
		mux.HandleFunc("/communities", srv.HandleCommunities)
		mux.HandleFunc("/admin/re-embed", srv.HandleReEmbed)

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

		// Sprint 4: schema fingerprint check at startup.
		if stored, current, err := CheckSchemaFingerprint(db, cfg.Schema); err != nil {
			slog.Warn("schema fingerprint check failed", "error", err)
		} else if stored != "" && stored != current {
			slog.Warn("schema fingerprint mismatch",
				"stored", stored,
				"current", current,
			)
		}

		go func() {
			slog.Info("server ready",
				"event", "server_ready",
				"port", port,
			)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server error: %v", err)
			}
		}()

		// Sprint 4: SIGHUP loop for dynamic config reload.
		// Runs in the background, reloads hermem.ini on SIGHUP,
		// validates the new config, and atomically swaps the
		// server's schema state. A failed reload logs an error
		// but does not crash the server.
		go func() {
			sighup := make(chan os.Signal, 1)
			signal.Notify(sighup, syscall.SIGHUP)
			for range sighup {
				slog.Info("SIGHUP received, reloading config")
				newCfg, err := LoadConfigFromBinaryDir()
				if err != nil {
					slog.Error("SIGHUP reload failed: cannot load config", "error", err)
					continue
				}
				if err := newCfg.Validate(); err != nil {
					slog.Error("SIGHUP reload failed: invalid config", "error", err)
					continue
				}
				srv.ReloadState(newCfg)
				if err := StoreSchemaFingerprint(db, newCfg.Schema); err != nil {
					slog.Warn("SIGHUP: failed to store schema fingerprint", "error", err)
				}
				slog.Info("SIGHUP reload complete",
					"schema_fingerprint", HashSchema(newCfg.Schema),
				)
			}
		}()

		<-quit
		slog.Info("shutting down...", "event", "server_shutdown")

		// 1. Stop accepting requests
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("http shutdown", "event", "shutdown_error", "error", err)
		}
		cancel()

		// 2. Cancel GC and wait for cycle to finish
		gcCancel()
		<-gcDone

		slog.Info("server stopped", "event", "server_stopped")

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}
