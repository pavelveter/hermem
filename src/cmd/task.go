package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/store"
)

// --- task-* subcommands grouped because they're all
//                  schema-aware task CRUD with shared shapes. ---

func init() {
	Register("task-status", cliTaskStatus)
	Register("task-list", cliTaskList)
	Register("task-show", cliTaskShow)
	Register("task-dep", cliTaskDep)
	Register("task-tree", cliTaskTree)
	Register("task-create", cliTaskCreate)
	Register("task-rollback", cliTaskRollback)
	Register("task-executable", cliTaskExecutable)
	// Alias — same handler dispatched under a friendlier name.
	Register("task-next", cliTaskExecutable)
}

func cliTaskStatus(env Env) {
	var req core.TaskStatusRequest
	DecodeStdin(&req)
	if req.ID == "" || req.Status == "" {
		log.Fatal("id, status required")
	}
	if err := store.SetStatus(env.DB, env.Cfg.Schema, req.ID, req.Status); err != nil {
		log.Fatalf("status: %v", err)
	}
	fmt.Println(`{"status":"ok"}`)
}

func cliTaskList(env Env) {
	var req core.TaskListRequest
	DecodeStdin(&req)
	tasks, err := store.ListTasks(env.DB, env.Cfg.Schema, req.Status, req.GoalID)
	if err != nil {
		log.Fatalf("list: %v", err)
	}
	if tasks == nil {
		tasks = []core.Entity{}
	}
	_ = writeJSON(os.Stdout, core.TaskExecutableResponse{Tasks: tasks})
}

func cliTaskShow(env Env) {
	var req core.TaskShowRequest
	DecodeStdin(&req)
	if req.ID == "" {
		log.Fatal("id required")
	}
	entity, blocked, recovers, err := store.GetTaskWithRelations(env.DB, env.Cfg.Schema, req.ID)
	if err != nil {
		log.Fatalf("show: %v", err)
	}
	_ = writeJSON(os.Stdout, core.TaskShowResponse{Entity: entity, BlockedBy: blocked, RecoversVia: recovers})
}

func cliTaskDep(env Env) {
	var req core.TaskDepRequest
	DecodeStdin(&req)
	if req.SourceID == "" || req.TargetID == "" {
		log.Fatal("source_id, target_id required")
	}
	rel := req.RelationType
	if rel == "" {
		rel = env.Cfg.Schema.RelationBlocking
	}
	if err := env.Cfg.ValidateRelation(rel); err != nil {
		log.Fatalf("invalid: %v", err)
	}
	if req.Add {
		_ = store.AddEdge(env.DB, req.SourceID, req.TargetID, rel, 1.0)
	} else {
		_ = store.DeleteEdge(env.DB, req.SourceID, req.TargetID, rel)
	}
	fmt.Println(`{"status":"ok"}`)
}

func cliTaskTree(env Env) {
	var req core.TaskTreeRequest
	DecodeStdin(&req)
	nodes, err := store.GetTaskTree(env.DB, env.Cfg.Schema, req.GoalID)
	if err != nil {
		log.Fatalf("tree: %v", err)
	}
	fmt.Print(store.RenderTaskTree(nodes, ""))
}

func cliTaskCreate(env Env) {
	var req core.TaskCreateRequest
	DecodeStdin(&req)
	if req.Content == "" {
		log.Fatal("content required")
	}
	if req.ID == "" {
		req.ID = core.NewTaskID()
	}
	emb, err := env.Embedder.Embed(env.Ctx, req.Content)
	if err != nil {
		log.Fatalf("embed: %v", err)
	}
	cat := config.FirstStatefulCategory(env.Cfg.Schema)
	if cat == "" {
		log.Fatal("no stateful category configured")
	}
	entity := core.Entity{ID: req.ID, Category: cat, Content: req.Content, Embedding: emb}
	if err := store.StoreEntityWithEmbedding(env.DB, env.VI, env.Cfg.Schema, entity); err != nil {
		log.Fatalf("store: %v", err)
	}
	fmt.Println(`{"status":"ok"}`)
}

func cliTaskRollback(env Env) {
	var req core.TaskRollbackRequest
	DecodeStdin(&req)
	if req.ID == "" {
		log.Fatal("id required")
	}
	rollbackID, err := store.FindRollbackTask(env.DB, env.Cfg.Schema, req.ID)
	if err != nil {
		log.Fatalf("rollback: %v", err)
	}
	_ = writeJSON(os.Stdout, core.TaskRollbackResponse{RollbackTaskID: rollbackID})
}

func cliTaskExecutable(env Env) {
	// Empty stdin ⇒ silently default to {} so this CLI is friendly when
	// piped from a tool that didn't write a body.
	data := ReadStdin()
	if data == "" {
		data = "{}"
	}
	var req struct {
		GoalID string `json:"goal_id"`
	}
	DecodeString(data, &req)
	tasks, err := retrieval.GetExecutableTasks(env.DB, env.Cfg.Schema, req.GoalID)
	if err != nil {
		log.Fatalf("executable: %v", err)
	}
	if tasks == nil {
		tasks = []core.Entity{}
	}
	_ = writeJSON(os.Stdout, core.TaskExecutableResponse{Tasks: tasks})
}
