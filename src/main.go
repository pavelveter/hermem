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

	"github.com/pavelveter/hermem/src/internal/algo"
	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/ingestion"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/server"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `hermem — knowledge graph server and CLI

Usage: hermem <command> [args]

Commands:
  store        Store an entity (JSON stdin)
  search       Vector search (JSON stdin)
  query        Full retrieval query (JSON stdin)
  edge         Create edge (JSON stdin)
  ingest       Ingest dialog (JSON stdin)
  task-status  Update task status (JSON stdin)
  task-executable / task-next  List executable tasks
  task-list    List tasks (JSON stdin)
  task-show    Show task details (JSON stdin)
  task-dep     Add/remove dependency (JSON stdin)
  task-tree    Show task tree (JSON stdin)
  task-create  Create task (JSON stdin)
  task-rollback Find rollback task (JSON stdin)
  temporal     Temporal retrieval (JSON stdin)
  timeline     Timeline of recent entities
  contradictions List contradictions [entity-id]
  explain      Explainable retrieval (JSON stdin)
  agent-loop   Agent execution loop (JSON stdin)
  verify       Graph integrity checker
  migrate      Show migration status
  migration-rollback  Rollback last migration
  migration-verify    Verify migration checksums
  execution-plan  Execution plan for a goal (JSON stdin)
  recovery-plan   Recovery plan (JSON stdin)
  connected-components  Find connected components
  provenance    Query by provenance
  communities   Community detection
  re-embed      Re-embed all entities
  quantize      Quantize an embedding (JSON stdin)
  schema        Show schema fingerprint
  serve [port]  Start HTTP server (default :8420)
`)
}

func main() {
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

	cfg, err := config.LoadConfigFromBinaryDir()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := store.InitDB(config.ResolveDBPath(cfg.DBPath), cfg.VectorDim)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	metrics.InitMetricsDB(db)
	vi := vector.NewIndex(cfg.VectorBackend, db, cfg.VectorDim)
	mw := metrics.InitMetricsWorker(db)
	defer mw.Stop()

	embedder := cfg.NewEmbedder()
	extractor := cfg.NewExtractor()
	reranker := cfg.NewReranker()

	cmd := os.Args[1]
	ctx := context.Background()

	switch cmd {
	case "store":
		handleStore(ctx, cfg, db, vi, embedder)
	case "search":
		handleSearch(ctx, db, vi, embedder)
	case "query":
		handleQuery(ctx, cfg, db, vi, embedder, reranker)
	case "edge":
		handleEdge(ctx, cfg, db, vi, embedder)
	case "ingest":
		handleIngest(ctx, cfg, db, vi, embedder, extractor)
	case "task-status":
		handleTaskStatus(cfg, db)
	case "task-executable", "task-next":
		handleTaskExecutable(cfg, db)
	case "task-list":
		handleTaskList(cfg, db)
	case "task-show":
		handleTaskShow(cfg, db)
	case "task-dep":
		handleTaskDep(cfg, db)
	case "task-tree":
		handleTaskTree(cfg, db)
	case "task-create":
		handleTaskCreate(ctx, cfg, db, vi, embedder)
	case "task-rollback":
		handleTaskRollback(cfg, db)
	case "temporal":
		handleTemporal(ctx, cfg, db, vi, embedder, reranker)
	case "timeline":
		handleTimeline(ctx, db)
	case "contradictions":
		handleContradictions(db)
	case "explain":
		handleExplain(ctx, cfg, db, vi, embedder, reranker)
	case "agent-loop":
		handleAgentLoop(ctx, cfg, db)
	case "verify":
		handleVerify(db, cfg)
	case "migrate":
		handleMigrate(db)
	case "migration-rollback":
		handleMigrationRollback(db)
	case "migration-verify":
		handleMigrationVerify(db)
	case "execution-plan":
		handleExecutionPlan(cfg, db)
	case "recovery-plan":
		handleRecoveryPlan(cfg, db)
	case "connected-components":
		handleConnectedComponents(db)
	case "provenance":
		handleProvenance(db)
	case "communities":
		handleCommunities(db)
	case "re-embed":
		handleReEmbed(ctx, cfg, db, vi, embedder)
	case "quantize":
		handleQuantize()
	case "schema":
		handleSchema(db, cfg)
	case "serve":
		handleServe(cfg, db, vi, embedder, extractor, reranker)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

func readStdin() string {
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		log.Fatal("stdin required")
	}
	data, _ := io.ReadAll(os.Stdin)
	return strings.TrimSpace(string(data))
}

func decodeStdin(v interface{}) {
	if code, field, msg, ok := server.DecodeStrict(bytes.NewReader([]byte(readStdin())), v); !ok {
		log.Fatalf("invalid request: %s (code=%s field=%s)", msg, code, field)
	}
}

// --- CLI command handlers ---

func handleStore(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder) {
	var req core.StoreRequest
	decodeStdin(&req)
	if req.ID == "" || req.Category == "" || req.Content == "" {
		log.Fatal("id, category, content required")
	}
	if err := cfg.ValidateCategory(req.Category); err != nil {
		log.Fatalf("invalid: %v", err)
	}
	if len(req.Embedding) == 0 {
		var err error
		req.Embedding, err = embedder.Embed(ctx, req.Content)
		if err != nil {
			log.Fatalf("embed: %v", err)
		}
	}
	if err := store.StoreEntityWithEmbedding(db, vi, cfg.Schema, core.Entity{ID: req.ID, Category: req.Category, Content: req.Content, Embedding: req.Embedding}); err != nil {
		log.Fatalf("store: %v", err)
	}
	vector.AutoLinkEdges(ctx, db, vi, embedder, req.ID, req.Embedding)
	fmt.Println(`{"status":"ok"}`)
}

func handleSearch(ctx context.Context, db *sql.DB, vi core.VectorIndex, embedder core.Embedder) {
	var req core.SearchRequest
	decodeStdin(&req)
	if req.Query == "" {
		log.Fatal("query required")
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	emb, err := embedder.Embed(ctx, req.Query)
	if err != nil {
		log.Fatalf("embed: %v", err)
	}
	results, err := vector.SearchByVector(db, vi, emb, req.TopK)
	if err != nil {
		log.Fatalf("search: %v", err)
	}
	json.NewEncoder(os.Stdout).Encode(results)
}

func handleQuery(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, reranker core.Reranker) {
	var req core.SearchRequest
	decodeStdin(&req)
	if req.Query == "" {
		log.Fatal("query required")
	}
	if req.TopK <= 0 {
		req.TopK = 3
	}
	emb, _ := embedder.Embed(ctx, req.Query)
	results, _ := vector.SearchByVector(db, vi, emb, req.TopK)
	var seedIDs []string
	for _, r := range results {
		seedIDs = append(seedIDs, r.Entity.ID)
	}
	opts := core.RetrieveContextOptions{MaxDepth: 2, DepthCeiling: cfg.MaxDepthCeiling, MaxRetrievedNodes: cfg.MaxRetrievedNodes, QueryEmbedding: emb, QueryText: req.Query, Ctx: ctx, RankingWeight: cfg.Ranking, Reranker: reranker}
	ctxResult, err := retrieval.RetrieveContext(db, seedIDs, opts)
	if err != nil {
		log.Fatalf("retrieve: %v", err)
	}
	json.NewEncoder(os.Stdout).Encode(map[string]string{"context": retrieval.FormatContextMarkdown(ctxResult)})
}

func handleEdge(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder) {
	var req core.EdgeRequest
	decodeStdin(&req)
	if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
		log.Fatal("source_id, target_id, relation_type required")
	}
	if err := cfg.ValidateRelation(req.RelationType); err != nil {
		log.Fatalf("invalid: %v", err)
	}
	if req.AutoCreate {
		vector.AddEdgeWithAutoCreate(ctx, db, vi, embedder, req.SourceID, req.TargetID, req.RelationType)
	} else {
		store.AddEdge(db, req.SourceID, req.TargetID, req.RelationType, req.Weight)
	}
	fmt.Println(`{"status":"ok"}`)
}

func handleIngest(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, extractor core.LLMExtractor) {
	var req core.IngestRequest
	decodeStdin(&req)
	if req.Dialog == "" {
		log.Fatal("dialog required")
	}
	w := ingestion.NewIngestionWorker(db, vi, extractor, embedder, cfg.DedupThreshold, cfg.Schema)
	if err := w.ProcessDialog(ctx, req.Dialog); err != nil {
		log.Fatalf("ingest: %v", err)
	}
	fmt.Println(`{"status":"ok"}`)
}

func handleTaskStatus(cfg *config.Config, db *sql.DB) {
	var req core.TaskStatusRequest
	decodeStdin(&req)
	if req.ID == "" || req.Status == "" {
		log.Fatal("id, status required")
	}
	if err := store.SetStatus(db, cfg.Schema, req.ID, req.Status); err != nil {
		log.Fatalf("status: %v", err)
	}
	fmt.Println(`{"status":"ok"}`)
}

func handleTaskExecutable(cfg *config.Config, db *sql.DB) {
	data := readStdin()
	if data == "" {
		data = "{}"
	}
	var req struct {
		GoalID string `json:"goal_id"`
	}
	server.DecodeStrict(bytes.NewReader([]byte(data)), &req)
	tasks, err := retrieval.GetExecutableTasks(db, cfg.Schema, req.GoalID)
	if err != nil {
		log.Fatalf("executable: %v", err)
	}
	if tasks == nil {
		tasks = []core.Entity{}
	}
	json.NewEncoder(os.Stdout).Encode(core.TaskExecutableResponse{Tasks: tasks})
}

func handleTaskList(cfg *config.Config, db *sql.DB) {
	var req core.TaskListRequest
	decodeStdin(&req)
	tasks, err := store.ListTasks(db, cfg.Schema, req.Status, req.GoalID)
	if err != nil {
		log.Fatalf("list: %v", err)
	}
	if tasks == nil {
		tasks = []core.Entity{}
	}
	json.NewEncoder(os.Stdout).Encode(core.TaskExecutableResponse{Tasks: tasks})
}

func handleTaskShow(cfg *config.Config, db *sql.DB) {
	var req core.TaskShowRequest
	decodeStdin(&req)
	if req.ID == "" {
		log.Fatal("id required")
	}
	entity, blocked, recovers, err := store.GetTaskWithRelations(db, cfg.Schema, req.ID)
	if err != nil {
		log.Fatalf("show: %v", err)
	}
	json.NewEncoder(os.Stdout).Encode(core.TaskShowResponse{Entity: entity, BlockedBy: blocked, RecoversVia: recovers})
}

func handleTaskDep(cfg *config.Config, db *sql.DB) {
	var req core.TaskDepRequest
	decodeStdin(&req)
	if req.SourceID == "" || req.TargetID == "" {
		log.Fatal("source_id, target_id required")
	}
	rel := req.RelationType
	if rel == "" {
		rel = cfg.Schema.RelationBlocking
	}
	if err := cfg.ValidateRelation(rel); err != nil {
		log.Fatalf("invalid: %v", err)
	}
	if req.Add {
		store.AddEdge(db, req.SourceID, req.TargetID, rel, 1.0)
	} else {
		store.DeleteEdge(db, req.SourceID, req.TargetID, rel)
	}
	fmt.Println(`{"status":"ok"}`)
}

func handleTaskTree(cfg *config.Config, db *sql.DB) {
	var req core.TaskTreeRequest
	decodeStdin(&req)
	nodes, err := store.GetTaskTree(db, cfg.Schema, req.GoalID)
	if err != nil {
		log.Fatalf("tree: %v", err)
	}
	fmt.Print(store.RenderTaskTree(nodes, ""))
}

func handleTaskCreate(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder) {
	var req core.TaskCreateRequest
	decodeStdin(&req)
	if req.Content == "" {
		log.Fatal("content required")
	}
	if req.ID == "" {
		req.ID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	}
	emb, err := embedder.Embed(ctx, req.Content)
	if err != nil {
		log.Fatalf("embed: %v", err)
	}
	cat := config.FirstStatefulCategory(cfg.Schema)
	if cat == "" {
		log.Fatal("no stateful category configured")
	}
	entity := core.Entity{ID: req.ID, Category: cat, Content: req.Content, Embedding: emb}
	if err := store.StoreEntityWithEmbedding(db, vi, cfg.Schema, entity); err != nil {
		log.Fatalf("store: %v", err)
	}
	fmt.Println(`{"status":"ok"}`)
}

func handleTaskRollback(cfg *config.Config, db *sql.DB) {
	var req core.TaskRollbackRequest
	decodeStdin(&req)
	if req.ID == "" {
		log.Fatal("id required")
	}
	rollbackID, err := store.FindRollbackTask(db, cfg.Schema, req.ID)
	if err != nil {
		log.Fatalf("rollback: %v", err)
	}
	json.NewEncoder(os.Stdout).Encode(core.TaskRollbackResponse{RollbackTaskID: rollbackID})
}

func handleTemporal(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, reranker core.Reranker) {
	var req struct {
		Query    string `json:"query"`
		TimeFrom string `json:"time_from"`
		TimeTo   string `json:"time_to"`
		TopK     int    `json:"top_k"`
	}
	decodeStdin(&req)
	if req.Query == "" {
		log.Fatal("query required")
	}
	if req.TopK <= 0 {
		req.TopK = 3
	}
	emb, _ := embedder.Embed(ctx, req.Query)
	results, _ := vector.SearchByVector(db, vi, emb, req.TopK)
	var seedIDs []string
	for _, r := range results {
		seedIDs = append(seedIDs, r.Entity.ID)
	}
	opts := core.RetrieveContextOptions{MaxDepth: 2, DepthCeiling: cfg.MaxDepthCeiling, MaxRetrievedNodes: cfg.MaxRetrievedNodes, QueryEmbedding: emb, QueryText: req.Query, RankingWeight: cfg.Ranking, Reranker: reranker}
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
	result, err := retrieval.RetrieveContext(db, seedIDs, opts)
	if err != nil {
		log.Fatalf("temporal: %v", err)
	}
	json.NewEncoder(os.Stdout).Encode(result)
}

func handleTimeline(ctx context.Context, db *sql.DB) {
	limit := 50
	rows, _ := db.QueryContext(ctx, `SELECT id, category, content, created_at FROM entities WHERE archived = 0 AND created_at IS NOT NULL ORDER BY created_at DESC LIMIT ?`, limit)
	defer rows.Close()
	for rows.Next() {
		var id, cat, content string
		var ts sql.NullTime
		rows.Scan(&id, &cat, &content, &ts)
		fmt.Printf("[%s] %s  %s  [%s]\n", ts.Time.Format(time.RFC3339), id, content, cat)
	}
}

func handleContradictions(db *sql.DB) {
	id := ""
	if len(os.Args) > 2 {
		id = os.Args[2]
	}
	pairs, err := store.GetContradictions(db, id)
	if err != nil {
		log.Fatalf("contradictions: %v", err)
	}
	for _, p := range pairs {
		fmt.Printf("[%s] %s\n  contradicts [%s] %s\n\n", p.SourceID, p.SourceContent, p.TargetID, p.TargetContent)
	}
}

func handleExplain(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, reranker core.Reranker) {
	var req core.SearchRequest
	decodeStdin(&req)
	if req.Query == "" {
		log.Fatal("query required")
	}
	emb, _ := embedder.Embed(ctx, req.Query)
	results, _ := vector.SearchByVector(db, vi, emb, 3)
	var seedIDs []string
	for _, r := range results {
		seedIDs = append(seedIDs, r.Entity.ID)
	}
	opts := core.RetrieveContextOptions{MaxDepth: 2, DepthCeiling: cfg.MaxDepthCeiling, MaxRetrievedNodes: cfg.MaxRetrievedNodes, QueryEmbedding: emb, QueryText: req.Query, Ctx: ctx, Explain: true, RankingWeight: cfg.Ranking, Reranker: reranker}
	result, err := retrieval.RetrieveContext(db, seedIDs, opts)
	if err != nil {
		log.Fatalf("explain: %v", err)
	}
	json.NewEncoder(os.Stdout).Encode(result)
}

func handleAgentLoop(ctx context.Context, cfg *config.Config, db *sql.DB) {
	var req struct {
		GoalID string `json:"goal_id"`
	}
	decodeStdin(&req)
	if req.GoalID == "" {
		log.Fatal("goal_id required")
	}
	slog.Info("agent loop started", "goal_id", req.GoalID)
	err := algo.AgentLoop(ctx, db, cfg.Schema, req.GoalID, func(ctx context.Context, task core.Entity) error {
		fmt.Printf("[%s] %s  [%s]\n", task.ID, task.Content, task.Category)
		return nil
	})
	if err != nil {
		log.Fatalf("agent loop: %v", err)
	}
}

func handleVerify(db *sql.DB, cfg *config.Config) {
	report, err := algo.VerifyGraph(db, cfg.Schema, cfg.VectorDim)
	if err != nil {
		log.Fatalf("verify: %v", err)
	}
	fmt.Print(report.String())
	if !report.Pass() {
		os.Exit(1)
	}
}

func handleMigrate(db *sql.DB) {
	status, err := store.MigrationStatus(db)
	if err != nil {
		log.Fatalf("migrate status: %v", err)
	}
	for _, m := range status {
		mark := "  "
		if m.Applied {
			mark = "OK"
		} else {
			mark = "--"
		}
		fmt.Printf("[%s] %s", mark, m.Name)
		if m.AppliedAt != "" {
			fmt.Printf("  (%s)", m.AppliedAt)
		}
		fmt.Println()
	}
}

func handleMigrationRollback(db *sql.DB) {
	name, err := store.RollbackMigration(db)
	if err != nil {
		log.Fatalf("rollback: %v", err)
	}
	if name == "" {
		fmt.Println("No migrations.")
	} else {
		fmt.Printf("Rolled back: %s\n", name)
	}
}

func handleMigrationVerify(db *sql.DB) {
	mismatches, err := store.VerifyMigrationIntegrity(db)
	if err != nil {
		log.Fatalf("verify: %v", err)
	}
	if len(mismatches) == 0 {
		fmt.Println("All migration checksums intact.")
	} else {
		fmt.Printf("%d mismatch(es)\n", len(mismatches))
		os.Exit(1)
	}
}

func handleExecutionPlan(cfg *config.Config, db *sql.DB) {
	var req struct {
		GoalID string `json:"goal_id"`
	}
	decodeStdin(&req)
	if req.GoalID == "" {
		log.Fatal("goal_id required")
	}
	tasks, err := algo.ExecutionPlan(db, cfg.Schema, req.GoalID)
	if err != nil {
		log.Fatalf("plan: %v", err)
	}
	for _, t := range tasks {
		fmt.Printf("[%s] %s  [%s]\n", t.ID, t.Content, t.Status)
	}
}

func handleRecoveryPlan(cfg *config.Config, db *sql.DB) {
	var req struct {
		ID string `json:"id"`
	}
	decodeStdin(&req)
	if req.ID == "" {
		log.Fatal("id required")
	}
	plan, err := store.GenerateRecoveryPlan(db, cfg.Schema, req.ID)
	if err != nil {
		log.Fatalf("recovery: %v", err)
	}
	for i, t := range plan {
		fmt.Printf("%d. [%s] %s  [%s]\n", i+1, t.ID, t.Content, t.Status)
	}
}

func handleConnectedComponents(db *sql.DB) {
	minSize := 2
	components, err := store.FindConnectedComponents(db, minSize)
	if err != nil {
		log.Fatalf("components: %v", err)
	}
	for _, c := range components {
		fmt.Printf("Component (size=%d, avg_degree=%.1f): %v\n", c.Size, c.AvgDegree, c.IDs)
	}
}

func handleProvenance(db *sql.DB) {
	args := os.Args[2:]
	var convID, msgID, source string
	limit := 50
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--conversation":
			convID = args[i+1]
			i++
		case "--message":
			msgID = args[i+1]
			i++
		case "--source":
			source = args[i+1]
			i++
		case "--limit":
			fmt.Sscanf(args[i+1], "%d", &limit)
			i++
		}
	}
	entities, err := store.GetEntitiesByProvenance(db, convID, msgID, source, limit)
	if err != nil {
		log.Fatalf("provenance: %v", err)
	}
	for _, e := range entities {
		fmt.Printf("[%s] %s  [%s]  conv=%s msg=%s\n", e.ID, e.Category, e.Content, e.ConversationID, e.MessageID)
	}
}

func handleCommunities(db *sql.DB) {
	comms, globalQ, err := store.DetectCommunities(db, 50)
	if err != nil {
		log.Fatalf("communities: %v", err)
	}
	fmt.Printf("Global modularity: %.6f\n", globalQ)
	for _, c := range comms {
		fmt.Printf("[%s] size=%d modularity=%.6f\n", c.ID, c.Size, c.Modularity)
	}
}

func handleReEmbed(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder) {
	batchSize := 50
	model := ""
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--batch-size" {
			fmt.Sscanf(args[i+1], "%d", &batchSize)
			i++
		}
		if args[i] == "--model" {
			model = args[i+1]
			i++
		}
	}
	result, err := algo.ReEmbedAll(ctx, db, vi, embedder, cfg.VectorDim, batchSize, model)
	if err != nil {
		log.Fatalf("re-embed: %v", err)
	}
	fmt.Printf("Re-embed: %d/%d entities (failed=%d, batches=%d, elapsed=%s)\n", result.ReEmbedded, result.TotalEntities, result.Failed, result.Batches, result.Elapsed)
}

func handleQuantize() {
	var req struct {
		Embedding []float32 `json:"embedding"`
	}
	decodeStdin(&req)
	if len(req.Embedding) == 0 {
		log.Fatal("embedding required")
	}
	qv := vector.QuantizeVector(req.Embedding)
	deq := vector.DequantizeVector(qv)
	fmt.Printf("Original: %d elements (%d bytes)\n", len(req.Embedding), len(req.Embedding)*4)
	fmt.Printf("Quantized: %d bytes (%.1fx)\n", 8+len(qv.Codes), float64(len(req.Embedding)*4)/float64(8+len(qv.Codes)))
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
	fmt.Printf("Max error: %.6f\n", maxErr)
}

func handleSchema(db *sql.DB, cfg *config.Config) {
	stored, current, err := store.CheckSchemaFingerprint(db, cfg.Schema)
	if err != nil {
		log.Fatalf("schema: %v", err)
	}
	fmt.Printf("Current: %s\nStored:   %s\n", current, stored)
	if stored != "" && stored != current {
		fmt.Println("WARNING: schema changed!")
	}
}

func handleServe(cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, extractor core.LLMExtractor, reranker core.Reranker) {
	port := "8420"
	if len(os.Args) > 2 {
		port = os.Args[2]
	}

	srv := server.NewServer(db, vi, embedder, extractor, cfg.DedupThreshold, core.RetrieveContextOptions{
		DepthCeiling: cfg.MaxDepthCeiling, MaxRetrievedNodes: cfg.MaxRetrievedNodes, RankingWeight: cfg.Ranking, Reranker: reranker,
	}, cfg.Schema)

	gcCtx, gcCancel := context.WithCancel(context.Background())
	gcDone := make(chan struct{})
	go func() { algo.GarbageCollector(gcCtx, db, vi, cfg.Retention); close(gcDone) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.HandleHealth)
	mux.HandleFunc("/health/live", srv.HandleHealthLive)
	mux.HandleFunc("/health/ready", srv.HandleHealthReady)
	mux.HandleFunc("/metrics", metrics.MetricsHandler)
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
	mux.HandleFunc("/timeline", srv.HandleTimeline)
	mux.HandleFunc("/provenance", srv.HandleProvenance)
	mux.HandleFunc("/recovery-plan", srv.HandleRecoveryPlan)
	mux.HandleFunc("/connected-components", srv.HandleConnectedComponents)
	mux.HandleFunc("/communities", srv.HandleCommunities)
	mux.HandleFunc("/admin/re-embed", srv.HandleReEmbed)

	apiKey := cfg.APIKey
	var handler http.Handler = mux
	handler = slogMiddleware(handler)
	handler = requestIDMiddleware(authMiddleware(apiKey)(handler))
	handler = recoveryMiddleware(handler)

	srvHTTP := &http.Server{Addr: ":" + port, Handler: handler, ReadTimeout: 5 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 120 * time.Second}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("server ready", "port", port)
		if err := srvHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	go func() {
		sighup := make(chan os.Signal, 1)
		signal.Notify(sighup, syscall.SIGHUP)
		for range sighup {
			slog.Info("SIGHUP reloading config")
			newCfg, err := config.LoadConfigFromBinaryDir()
			if err != nil {
				slog.Error("SIGHUP: load config", "err", err)
				continue
			}
			if err := newCfg.Validate(); err != nil {
				slog.Error("SIGHUP: invalid config", "err", err)
				continue
			}
			srv.ReloadState(newCfg.Schema, newCfg.Ranking, newCfg.NewReranker())
			store.StoreSchemaFingerprint(db, newCfg.Schema)
		}
	}()

	<-quit
	slog.Info("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	srvHTTP.Shutdown(shutdownCtx)
	cancel()
	gcCancel()
	<-gcDone
	slog.Info("server stopped")
}

// --- Middleware ---

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "err", rec)
				http.Error(w, "internal error", 500)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = fmt.Sprintf("%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-ID", reqID)
		next.ServeHTTP(w, r)
	})
}

func authMiddleware(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey != "" && r.Header.Get("X-API-Key") != apiKey {
				http.Error(w, `{"error":"unauthorized"}`, 401)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func slogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Debug("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}
