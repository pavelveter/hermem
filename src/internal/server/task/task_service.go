// Package task hosts the task-graph HTTP service: status, executable, list,
// show, dep, rollback, tree, create, recovery-plan.
//
// TaskCreate is the only handler that needs the embedder + vector-index
// (to embed the new task and auto-link it). The other handlers operate
// purely on the relational schema and the task DAG.
package task

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Service handles task-graph endpoints.
type Service struct {
	DB       *sql.DB
	VI       core.VectorIndex
	Embedder core.Embedder
	Metrics  *metrics.Metrics
	Refs     *serverstate.Ref
}

// New constructs a Service. m is the request-counter holder threaded
// from Env.Metrics; handler bumps go through s.Metrics.Inc* in the
// same goroutine that draws the per-request schema from s.Refs.Load().
func New(db *sql.DB, vi core.VectorIndex, embedder core.Embedder, m *metrics.Metrics, refs *serverstate.Ref) *Service {
	return &Service{DB: db, VI: vi, Embedder: embedder, Metrics: m, Refs: refs}
}

// Routes returns the URL → handler mapping for this service.
// Note: /task/executable and /task/next both route to HandleTaskExecutable —
// the second alias exists for legacy CLI frontends.
func (s *Service) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/task/status":     s.HandleTaskStatus,
		"/task/executable": s.HandleTaskExecutable,
		"/task/next":       s.HandleTaskExecutable, // alias
		"/task/list":       s.HandleTaskList,
		"/task/show":       s.HandleTaskShow,
		"/task/dep":        s.HandleTaskDep,
		"/task/tree":       s.HandleTaskTree,
		"/task/create":     s.HandleTaskCreate,
		"/task/rollback":   s.HandleTaskRollback,
		"/recovery-plan":   s.HandleRecoveryPlan,
	}
}

func (s *Service) HandleTaskStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskStatusRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.ID == "" || req.Status == "" {
		httputil.WriteError(w, http.StatusBadRequest, "id, status required")
		return
	}
	if err := store.SetStatus(s.DB, s.Refs.Load().Schema, req.ID, req.Status); err != nil {
		s.Metrics.IncErr()
		if strings.Contains(err.Error(), "not found") {
			httputil.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		httputil.WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.Metrics.IncTaskStatus()
	httputil.WriteJSON(w, http.StatusNoContent, nil)
}

func (s *Service) HandleTaskExecutable(w http.ResponseWriter, r *http.Request) {
	goals := r.URL.Query().Get("goal_id")
	state := s.Refs.Load()
	tasks, err := retrieval.GetExecutableTasks(s.DB, state.Schema, goals)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []core.Entity{}
	}
	s.Metrics.IncTaskExec()
	httputil.WriteJSON(w, http.StatusOK, core.TaskExecutableResponse{Tasks: tasks})
}

func (s *Service) HandleTaskList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskListRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	state := s.Refs.Load()
	tasks, err := store.ListTasks(s.DB, state.Schema, req.Status, req.GoalID)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []core.Entity{}
	}
	s.Metrics.IncTaskList()
	httputil.WriteJSON(w, http.StatusOK, core.TaskExecutableResponse{Tasks: tasks})
}

func (s *Service) HandleTaskShow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskShowRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.ID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "id required")
		return
	}
	state := s.Refs.Load()
	entity, blocked, recovers, err := store.GetTaskWithRelations(s.DB, state.Schema, req.ID)
	if err != nil {
		s.Metrics.IncErr()
		if strings.Contains(err.Error(), "not found") {
			httputil.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncTaskShow()
	httputil.WriteJSON(w, http.StatusOK, core.TaskShowResponse{Entity: entity, BlockedBy: blocked, RecoversVia: recovers})
}

func (s *Service) HandleTaskDep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskDepRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.SourceID == "" || req.TargetID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "source_id, target_id required")
		return
	}
	rel := req.RelationType
	state := s.Refs.Load()
	if rel == "" {
		rel = state.Schema.RelationBlocking
	}
	if !state.ValidRelationTypes[rel] {
		httputil.WriteError(w, http.StatusUnprocessableEntity, fmt.Sprintf("unknown relation_type: %s", rel))
		return
	}
	if req.Add {
		_ = store.AddEdge(s.DB, req.SourceID, req.TargetID, rel, 1.0)
	} else {
		_ = store.DeleteEdge(s.DB, req.SourceID, req.TargetID, rel)
	}
	s.Metrics.IncTaskDep()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Service) HandleTaskRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskRollbackRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.ID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "id required")
		return
	}
	rollbackID, err := store.FindRollbackTask(s.DB, s.Refs.Load().Schema, req.ID)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncTaskRollback()
	httputil.WriteJSON(w, http.StatusOK, core.TaskRollbackResponse{RollbackTaskID: rollbackID})
}

func (s *Service) HandleTaskTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskTreeRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	nodes, err := store.GetTaskTree(s.DB, s.Refs.Load().Schema, req.GoalID)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncTaskTree()
	httputil.WriteJSON(w, http.StatusOK, core.TaskTreeResponse{Tree: store.RenderTaskTree(nodes, "")})
}

func (s *Service) HandleTaskCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskCreateRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.Content == "" {
		httputil.WriteError(w, http.StatusBadRequest, "content required")
		return
	}
	if req.ID == "" {
		req.ID = core.NewTaskID()
	}
	emb, err := s.Embedder.Embed(r.Context(), req.Content)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	state := s.Refs.Load()
	cat := config.FirstStatefulCategory(state.Schema)
	if cat == "" {
		httputil.WriteError(w, http.StatusUnprocessableEntity, "no stateful category configured")
		return
	}
	entity := core.Entity{ID: req.ID, Category: cat, Content: req.Content, Embedding: emb}
	if err := store.StoreEntityWithEmbedding(s.DB, s.VI, state.Schema, entity); err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, cid := range req.ContextIDs {
		if cid != "" {
			_ = store.AddEdge(s.DB, req.ID, cid, "related_to", 1.0)
		}
	}
	vector.AutoLinkEdges(r.Context(), s.DB, s.VI, s.Embedder, req.ID, emb)
	s.Metrics.IncTaskCreate()
	httputil.WriteJSON(w, http.StatusOK, core.TaskCreateResponse{ID: req.ID, Status: "ok"})
}

func (s *Service) HandleRecoveryPlan(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		httputil.WriteError(w, http.StatusBadRequest, "id required")
		return
	}
	plan, err := store.GenerateRecoveryPlan(s.DB, s.Refs.Load().Schema, id)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if plan == nil {
		plan = []core.Entity{}
	}
	httputil.WriteJSON(w, http.StatusOK, plan)
}
