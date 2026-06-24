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
	"os"
	"os/signal"
	"strconv"
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

// Build-time variables injected via -ldflags:
//
//	-X main.version=$(VERSION)
//	-X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)
//	-X main.gitCommit=$(git rev-parse --short HEAD)
var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `hermem — knowledge graph server and CLI

Usage: hermem <command> [args]

Commands:
  store, search, query, edge, ingest, explain       Knowledge CRUD (JSON stdin)
  task-status, task-list, task-show, task-dep,      Task management (JSON stdin)
  task-tree, task-create, task-rollback,
  task-executable (alias: task-next)
  temporal       Temporal retrieval (JSON stdin)
  timeline       List recent entities
  contradictions List contradictions [entity-id]
  agent-loop     Agent execution loop (JSON stdin)
  verify         Graph integrity checker
  migrate, migration-rollback, migration-verify
  execution-plan, recovery-plan, connected-components, communities
  provenance     Query by provenance
  re-embed       Re-embed all entities
  quantize       Quantize an embedding locally (JSON stdin)
  schema         Show schema fingerprint
  serve [port]   Start HTTP server (default :8420)
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
	case "serve":
		cliServe(cfg, db, vi, embedder, extractor, reranker)
	case "store":
		cliStore(ctx, cfg, db, vi, embedder)
	case "search":
		cliSearch(ctx, db, vi, embedder)
	case "query":
		cliQuery(ctx, cfg, db, vi, embedder, reranker)
	case "edge":
		cliEdge(ctx, cfg, db, vi, embedder)
	case "ingest":
		cliIngest(ctx, cfg, db, vi, embedder, extractor)
	case "temporal":
		cliTemporal(ctx, cfg, db, vi, embedder, reranker)
	case "explain":
		cliExplain(ctx, cfg, db, vi, embedder, reranker)
	case "task-status":
		cliTaskStatus(cfg, db)
	case "task-list":
		cliTaskList(cfg, db)
	case "task-show":
		cliTaskShow(cfg, db)
	case "task-dep":
		cliTaskDep(cfg, db)
	case "task-tree":
		cliTaskTree(cfg, db)
	case "task-create":
		cliTaskCreate(ctx, cfg, db, vi, embedder)
	case "task-rollback":
		cliTaskRollback(cfg, db)
	case "task-executable", "task-next":
		cliTaskExecutable(cfg, db)
	case "timeline":
		cliTimeline(ctx, db)
	case "contradictions":
		cliContradictions(db)
	case "agent-loop":
		cliAgentLoop(ctx, cfg, db)
	case "verify":
		cliVerify(db, cfg)
	case "migrate":
		cliMigrate(db)
	case "migration-rollback":
		cliMigrationRollback(db)
	case "migration-verify":
		cliMigrationVerify(db)
	case "execution-plan":
		cliExecutionPlan(cfg, db)
	case "recovery-plan":
		cliRecoveryPlan(cfg, db)
	case "connected-components":
		cliConnectedComponents(db)
	case "provenance":
		cliProvenance(db)
	case "communities":
		cliCommunities(db)
	case "re-embed":
		cliReEmbed(ctx, cfg, db, vi, embedder)
	case "quantize":
		cliQuantize()
	case "schema":
		cliSchema(db, cfg)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		os.Exit(1)
	}
}

// --- stdin JSON helpers ---

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

// --- CLI command implementations (delegating to packages) ---

func cliStore(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder) {
	var req core.StoreRequest
	decodeStdin(&req)
	if req.ID == "" || req.Category == "" || req.Content == "" {
		log.Fatal("id, category, content required")
	}
	if err := cfg.ValidateCategory(req.Category); err != nil {
		log.Fatalf("invalid: %v", err)
	}
	if len(req.Embedding) == 0 {
		emb, err := embedder.Embed(ctx, req.Content)
		if err != nil {
			log.Fatalf("embed: %v", err)
		}
		req.Embedding = emb
	}
	if err := store.StoreEntityWithEmbedding(db, vi, cfg.Schema, core.Entity{ID: req.ID, Category: req.Category, Content: req.Content, Embedding: req.Embedding}); err != nil {
		log.Fatalf("store: %v", err)
	}
	fmt.Println(`{"status":"ok"}`)
}

func cliSearch(ctx context.Context, db *sql.DB, vi core.VectorIndex, embedder core.Embedder) {
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
	_ = json.NewEncoder(os.Stdout).Encode(results)
}

func cliQuery(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, reranker core.Reranker) {
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
	seedIDs := make([]string, 0, len(results))
	for _, r := range results {
		seedIDs = append(seedIDs, r.Entity.ID)
	}
	opts := core.RetrieveContextOptions{MaxDepth: 2, DepthCeiling: cfg.MaxDepthCeiling, MaxRetrievedNodes: cfg.MaxRetrievedNodes, QueryEmbedding: emb, QueryText: req.Query, Ctx: ctx, RankingWeight: cfg.Ranking, Reranker: reranker}
	ctxResult, err := retrieval.RetrieveContext(db, seedIDs, opts)
	if err != nil {
		log.Fatalf("retrieve: %v", err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"context": retrieval.FormatContextMarkdown(ctxResult)})
}

func cliEdge(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder) {
	var req core.EdgeRequest
	decodeStdin(&req)
	if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
		log.Fatal("source_id, target_id, relation_type required")
	}
	if err := cfg.ValidateRelation(req.RelationType); err != nil {
		log.Fatalf("invalid: %v", err)
	}
	var edgeErr error
	if req.AutoCreate {
		edgeErr = vector.AddEdgeWithAutoCreate(ctx, db, vi, embedder, req.SourceID, req.TargetID, req.RelationType)
	} else {
		edgeErr = store.AddEdge(db, req.SourceID, req.TargetID, req.RelationType, req.Weight)
	}
	if edgeErr != nil {
		log.Fatalf("edge: %v", edgeErr)
	}
	fmt.Println(`{"status":"ok"}`)
}

func cliIngest(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, extractor core.LLMExtractor) {
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

func cliTaskStatus(cfg *config.Config, db *sql.DB) {
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

func cliTaskExecutable(cfg *config.Config, db *sql.DB) {
	data := readStdin()
	if data == "" {
		data = "{}"
	}
	var req struct {
		GoalID string `json:"goal_id"`
	}
	_, _, _, _ = server.DecodeStrict(bytes.NewReader([]byte(data)), &req)
	tasks, err := retrieval.GetExecutableTasks(db, cfg.Schema, req.GoalID)
	if err != nil {
		log.Fatalf("executable: %v", err)
	}
	if tasks == nil {
		tasks = []core.Entity{}
	}
	_ = json.NewEncoder(os.Stdout).Encode(core.TaskExecutableResponse{Tasks: tasks})
}

func cliTaskList(cfg *config.Config, db *sql.DB) {
	var req core.TaskListRequest
	decodeStdin(&req)
	tasks, err := store.ListTasks(db, cfg.Schema, req.Status, req.GoalID)
	if err != nil {
		log.Fatalf("list: %v", err)
	}
	if tasks == nil {
		tasks = []core.Entity{}
	}
	_ = json.NewEncoder(os.Stdout).Encode(core.TaskExecutableResponse{Tasks: tasks})
}

func cliTaskShow(cfg *config.Config, db *sql.DB) {
	var req core.TaskShowRequest
	decodeStdin(&req)
	if req.ID == "" {
		log.Fatal("id required")
	}
	entity, blocked, recovers, err := store.GetTaskWithRelations(db, cfg.Schema, req.ID)
	if err != nil {
		log.Fatalf("show: %v", err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(core.TaskShowResponse{Entity: entity, BlockedBy: blocked, RecoversVia: recovers})
}

func cliTaskDep(cfg *config.Config, db *sql.DB) {
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
		_ = store.AddEdge(db, req.SourceID, req.TargetID, rel, 1.0)
	} else {
		_ = store.DeleteEdge(db, req.SourceID, req.TargetID, rel)
	}
	fmt.Println(`{"status":"ok"}`)
}

func cliTaskTree(cfg *config.Config, db *sql.DB) {
	var req core.TaskTreeRequest
	decodeStdin(&req)
	nodes, err := store.GetTaskTree(db, cfg.Schema, req.GoalID)
	if err != nil {
		log.Fatalf("tree: %v", err)
	}
	fmt.Print(store.RenderTaskTree(nodes, ""))
}

func cliTaskCreate(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder) {
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

func cliTaskRollback(cfg *config.Config, db *sql.DB) {
	var req core.TaskRollbackRequest
	decodeStdin(&req)
	if req.ID == "" {
		log.Fatal("id required")
	}
	rollbackID, err := store.FindRollbackTask(db, cfg.Schema, req.ID)
	if err != nil {
		log.Fatalf("rollback: %v", err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(core.TaskRollbackResponse{RollbackTaskID: rollbackID})
}

func cliTemporal(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, reranker core.Reranker) {
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
	seedIDs := make([]string, 0, len(results))
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
	_ = json.NewEncoder(os.Stdout).Encode(result)
}

func cliTimeline(ctx context.Context, db *sql.DB) {
	rows, _ := db.QueryContext(ctx, `SELECT id, category, content, created_at FROM entities WHERE archived = 0 AND created_at IS NOT NULL ORDER BY created_at DESC LIMIT 50`)
	defer rows.Close()
	for rows.Next() {
		var id, cat, content string
		var ts sql.NullTime
		_ = rows.Scan(&id, &cat, &content, &ts)
		fmt.Printf("[%s] %s  %s  [%s]\n", ts.Time.Format(time.RFC3339), id, content, cat)
	}
}

func cliContradictions(db *sql.DB) {
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

func cliExplain(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, reranker core.Reranker) {
	var req core.SearchRequest
	decodeStdin(&req)
	if req.Query == "" {
		log.Fatal("query required")
	}
	emb, _ := embedder.Embed(ctx, req.Query)
	results, _ := vector.SearchByVector(db, vi, emb, 3)
	seedIDs := make([]string, 0, len(results))
	for _, r := range results {
		seedIDs = append(seedIDs, r.Entity.ID)
	}
	opts := core.RetrieveContextOptions{MaxDepth: 2, DepthCeiling: cfg.MaxDepthCeiling, MaxRetrievedNodes: cfg.MaxRetrievedNodes, QueryEmbedding: emb, QueryText: req.Query, Ctx: ctx, Explain: true, RankingWeight: cfg.Ranking, Reranker: reranker}
	result, err := retrieval.RetrieveContext(db, seedIDs, opts)
	if err != nil {
		log.Fatalf("explain: %v", err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}

func cliAgentLoop(ctx context.Context, cfg *config.Config, db *sql.DB) {
	var req struct {
		GoalID string `json:"goal_id"`
	}
	decodeStdin(&req)
	if req.GoalID == "" {
		log.Fatal("goal_id required")
	}
	slog.Info("agent loop started", "goal_id", req.GoalID)
	err := algo.AgentLoop(ctx, db, cfg.Schema, req.GoalID, func(_ context.Context, task core.Entity) error {
		fmt.Printf("[%s] %s  [%s]\n", task.ID, task.Content, task.Category)
		return nil
	})
	if err != nil {
		log.Fatalf("agent loop: %v", err)
	}
}

func cliVerify(db *sql.DB, cfg *config.Config) {
	report, err := algo.VerifyGraph(db, cfg.Schema, cfg.VectorDim)
	if err != nil {
		log.Fatalf("verify: %v", err)
	}
	fmt.Print(report.String())
	if !report.Pass() {
		os.Exit(1)
	}
}

func cliMigrate(db *sql.DB) {
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

func cliMigrationRollback(db *sql.DB) {
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

func cliMigrationVerify(db *sql.DB) {
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

func cliExecutionPlan(cfg *config.Config, db *sql.DB) {
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

func cliRecoveryPlan(cfg *config.Config, db *sql.DB) {
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

func cliConnectedComponents(db *sql.DB) {
	components, err := store.FindConnectedComponents(db, 2)
	if err != nil {
		log.Fatalf("components: %v", err)
	}
	for _, c := range components {
		fmt.Printf("Component (size=%d, avg_degree=%.1f): %v\n", c.Size, c.AvgDegree, c.IDs)
	}
}

func cliProvenance(db *sql.DB) {
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
			limit, _ = strconv.Atoi(args[i+1])
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

func cliCommunities(db *sql.DB) {
	comms, globalQ, err := store.DetectCommunities(db, 50)
	if err != nil {
		log.Fatalf("communities: %v", err)
	}
	fmt.Printf("Global modularity: %.6f\n", globalQ)
	for _, c := range comms {
		fmt.Printf("[%s] size=%d modularity=%.6f\n", c.ID, c.Size, c.Modularity)
	}
}

func cliReEmbed(ctx context.Context, cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder) {
	batchSize := 50
	model := ""
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--batch-size" {
			batchSize, _ = strconv.Atoi(args[i+1])
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
	fmt.Printf("Re-embed: %d/%d entities (failed=%d, batches=%d, elapsed=%s)\n",
		result.ReEmbedded, result.TotalEntities, result.Failed, result.Batches, result.Elapsed)
}

func cliQuantize() {
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
	var maxErr float32
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

func cliSchema(db *sql.DB, cfg *config.Config) {
	stored, current, err := store.CheckSchemaFingerprint(db, cfg.Schema)
	if err != nil {
		log.Fatalf("schema: %v", err)
	}
	fmt.Printf("Current: %s\nStored:   %s\n", current, stored)
	if stored != "" && stored != current {
		fmt.Println("WARNING: schema changed!")
	}
}

// cliServe delegates the long HTTP server lifecycle to server.StartStandalone
// and only adds SIGHUP-driven config reload + version banner here.
func cliServe(cfg *config.Config, db *sql.DB, vi core.VectorIndex, embedder core.Embedder, extractor core.LLMExtractor, reranker core.Reranker) {
	port := "8420"
	if len(os.Args) > 2 {
		port = os.Args[2]
	}
	slog.Info("hermem starting", "port", port, "version", version, "build_date", buildDate, "git_commit", gitCommit)

	srv := server.NewServer(db, vi, embedder, extractor, cfg.DedupThreshold,
		core.RetrieveContextOptions{
			DepthCeiling: cfg.MaxDepthCeiling, MaxRetrievedNodes: cfg.MaxRetrievedNodes,
			RankingWeight: cfg.Ranking, Reranker: reranker,
		},
		cfg.Schema)

	// SIGHUP reload loop — separate from server lifecycle so we can re-validate config without restarting.
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for range sighup {
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
			_ = store.StoreSchemaFingerprint(db, newCfg.Schema)
			slog.Info("SIGHUP applied")
		}
	}()

	if err := server.StartStandalone(server.StartStandaloneConfig{
		DB:                db,
		VI:                vi,
		Embedder:          embedder,
		Extractor:         extractor,
		Reranker:          reranker,
		Schema:            cfg.Schema,
		Ranking:           cfg.Ranking,
		DedupThreshold:    cfg.DedupThreshold,
		DepthCeiling:      cfg.MaxDepthCeiling,
		MaxRetrievedNodes: cfg.MaxRetrievedNodes,
		Retention:         cfg.Retention,
		APIKey:            cfg.APIKey,
		Port:              port,
	}); err != nil {
		log.Fatalf("server: %v", err)
	}
}
