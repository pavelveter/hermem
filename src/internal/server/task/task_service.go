// Package task hosts the task-graph HTTP transport.
//
// Domain logic lives in src/internal/task (Service); this package owns
// transport-only concerns: JSON encoding, method checks, request-body
// limits, metric increments, and the route registry for /task/* and
// /recovery-plan.
//
// Following the same pattern as PHASE 2.1 (memory), 2.2 (retrieval),
// 2.3 (contradiction): HTTPService is a thin shell — parse → validate
// → call TaskSvc.* → write envelope. The domain Service has no
// knowledge of HTTP, httputil, serverstate.Ref, or metrics; the schema
// is loaded once per request via Refs and threaded into the domain.
package task

import (
	"net/http"
	"strings"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	tasksvc "github.com/pavelveter/hermem/src/internal/task"
)

// HTTPService is the transport shell for the task domain.
//
// Holds the domain Service (TaskSvc), the metrics counters (Metrics),
// and the serverstate.Ref for per-request schema reads. The shell
// decides which transport-level concerns (IncErr, IncTaskExec, etc.)
// fire on each response; the domain never sees them.
type HTTPService struct {
	TaskSvc *tasksvc.Service
	Metrics *metrics.Metrics
	Refs    *serverstate.Ref
}

// New constructs an HTTPService. In production cli/serve.go wires the
// domain Service from env.DB + env.Embedder + env.VI via
// task.NewService(...). Tests construct inline.
func New(svc *tasksvc.Service, m *metrics.Metrics, refs *serverstate.Ref) *HTTPService {
	return &HTTPService{TaskSvc: svc, Metrics: m, Refs: refs}
}

// Routes returns the URL → handler mapping for this service.
//
// Note: /task/executable and /task/next both route to
// HandleTaskExecutable — the second alias exists for legacy CLI
// frontends (cobra's `task next` uses the same handler).
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
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

// HandleTaskStatus — POST /task/status. Transitions one task's state.
func (s *HTTPService) HandleTaskStatus(w http.ResponseWriter, r *http.Request) {
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
	state := s.Refs.Load()
	if err := s.TaskSvc.Status(r.Context(), req.ID, req.Status, state.Schema); err != nil {
		s.Metrics.IncErr()
		// "task not found: <id>" → 400 (client mistake: wrong id).
		// Other errors → 422 (semantic violation: unknown state value).
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

// HandleTaskExecutable — GET /task/executable[?goal_id=X] (and /task/next).
//
// Returns TaskExecutableResponse{Tasks: ...}. Empty result normalized
// to `[]` at domain level; HTTP envelope stays consistent.
func (s *HTTPService) HandleTaskExecutable(w http.ResponseWriter, r *http.Request) {
	goals := r.URL.Query().Get("goal_id")
	state := s.Refs.Load()
	tasks, err := s.TaskSvc.Executable(r.Context(), goals, state.Schema)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncTaskExec()
	httputil.WriteJSON(w, http.StatusOK, core.TaskExecutableResponse{Tasks: tasks})
}

// HandleTaskList — POST /task/list. Returns TaskExecutableResponse{Tasks: ...}.
func (s *HTTPService) HandleTaskList(w http.ResponseWriter, r *http.Request) {
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
	tasks, err := s.TaskSvc.List(r.Context(), req.Status, req.GoalID, state.Schema)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncTaskList()
	httputil.WriteJSON(w, http.StatusOK, core.TaskExecutableResponse{Tasks: tasks})
}

// HandleTaskShow — POST /task/show. Returns TaskShowResponse.
func (s *HTTPService) HandleTaskShow(w http.ResponseWriter, r *http.Request) {
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
	entity, blocked, recovers, err := s.TaskSvc.Show(r.Context(), req.ID, state.Schema)
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

// HandleTaskDep — POST /task/dep. Adds or removes a dependency edge
// between two tasks. Pre-validation against state.ValidRelationTypes
// stays in the HTTP shell (it's about what the schema *currently*
// considers valid, not about the domain's own invariants).
func (s *HTTPService) HandleTaskDep(w http.ResponseWriter, r *http.Request) {
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
	state := s.Refs.Load()
	rel := req.RelationType
	if rel == "" {
		rel = state.Schema.RelationBlocking
	}
	if !state.ValidRelationTypes[rel] {
		httputil.WriteError(w, http.StatusUnprocessableEntity, "unknown relation_type: "+rel)
		return
	}
	if err := s.TaskSvc.Dep(r.Context(), req.SourceID, req.TargetID, rel, req.Add); err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncTaskDep()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleTaskRollback — POST /task/rollback. Finds the rollback
// companion task for a given task ID.
func (s *HTTPService) HandleTaskRollback(w http.ResponseWriter, r *http.Request) {
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
	rollbackID, err := s.TaskSvc.Rollback(r.Context(), req.ID, s.Refs.Load().Schema)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncTaskRollback()
	httputil.WriteJSON(w, http.StatusOK, core.TaskRollbackResponse{RollbackTaskID: rollbackID})
}

// HandleTaskTree — POST /task/tree. Returns the rendered tree string.
func (s *HTTPService) HandleTaskTree(w http.ResponseWriter, r *http.Request) {
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
	tree, err := s.TaskSvc.Tree(r.Context(), req.GoalID, s.Refs.Load().Schema)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncTaskTree()
	httputil.WriteJSON(w, http.StatusOK, core.TaskTreeResponse{Tree: tree})
}

// HandleTaskCreate — POST /task/create. Embeds content + stores + adds
// context edges + auto-links. ID generation (core.NewTaskID) stays in
// the HTTP shell: it's a transport-side concern (clients lacking IDs
// need the server to generate one before the domain call).
func (s *HTTPService) HandleTaskCreate(w http.ResponseWriter, r *http.Request) {
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
	state := s.Refs.Load()
	newID, err := s.TaskSvc.Create(r.Context(), req.ID, req.Content, req.ContextIDs, state.Schema)
	if err != nil {
		s.Metrics.IncErr()
		// "create: no stateful category configured" → 422 (semantic).
		if strings.Contains(err.Error(), "no stateful category") {
			httputil.WriteError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncTaskCreate()
	httputil.WriteJSON(w, http.StatusOK, core.TaskCreateResponse{ID: newID, Status: "ok"})
}

// HandleRecoveryPlan — GET /recovery-plan[?id=X]. Returns a list of
// recovery entities (or `[]` on empty). Note: this handler lives in
// the TASK HTTPService because recovery is task-only; no other shell
// owns it.
func (s *HTTPService) HandleRecoveryPlan(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		httputil.WriteError(w, http.StatusBadRequest, "id required")
		return
	}
	plan, err := s.TaskSvc.RecoveryPlan(r.Context(), id, s.Refs.Load().Schema)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, plan)
}
