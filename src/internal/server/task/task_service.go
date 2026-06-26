// Package task hosts the task-graph HTTP transport.
//
// Domain logic lives in src/internal/task (Service); this package owns
// transport-only concerns: JSON encoding, method checks, request-body
// limits, metric increments, and the route registry for /task/* and
// /recovery-plan.
//
// Following the same pattern as PHASE 2.1 (memory), 2.2 (retrieval),
// 2.3 (contradiction): HTTPService is a thin shell — parse → validate
// → call Svc.* → write envelope. The domain Service has no
// knowledge of HTTP, httputil, serverstate.Ref, or metrics; the schema
// is loaded once per request via Refs and threaded into the domain.
//
// §3.2 — embeds shared.BaseHTTPService. Eight of nine handlers route
// through s.Wrap so the IncErr + WriteError(500,...) boilerplate is
// collapsed. /task/status is the deliberate exception: its pre-§3.2
// contract mapped non-NotFound errors to 422 (semantic — "unknown
// state value") rather than 500, so HandleTaskStatus stays inline and
// Routes() registers it WITHOUT s.Wrap. The other handlers mapStatus'
// 422 branches (CodeInvalidInput, ErrInvalidInput) cover their bespoke
// needs correctly.
package task

import (
	"errors"
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/server/shared"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	tasksvc "github.com/pavelveter/hermem/src/internal/task"
)

// HTTPService is the transport shell for the task domain.
//
// Holds the domain Service (Svc) + the embedded BaseHTTPService
// (Metrics, Refs promoted).
type HTTPService struct {
	Svc *tasksvc.Service
	shared.BaseHTTPService
}

// New constructs an HTTPService. In production cli/serve.go wires the
// domain Service from env.DB + env.Embedder + env.VI via
// task.New(...). Tests construct inline.
func New(svc *tasksvc.Service, m *metrics.Metrics, refs *serverstate.Ref) *HTTPService {
	return &HTTPService{
		Svc: svc,
		BaseHTTPService: shared.BaseHTTPService{
			Metrics: m,
			Refs:    refs,
		},
	}
}

// Routes returns the URL → handler mapping for this service.
//
// /task/executable and /task/next both route to HandleTaskExecutable —
// the second alias exists for legacy CLI frontends. §3.2 wraps 8/9
// handlers; /task/status is the bespoke exception.
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/task/status":     s.HandleTaskStatus, // NOT wrapped — bespoke 422/400 mapping
		"/task/executable": s.Wrap(s.HandleTaskExecutable),
		"/task/next":       s.Wrap(s.HandleTaskExecutable), // alias
		"/task/list":       s.Wrap(s.HandleTaskList),
		"/task/show":       s.Wrap(s.HandleTaskShow),
		"/task/dep":        s.Wrap(s.HandleTaskDep),
		"/task/tree":       s.Wrap(s.HandleTaskTree),
		"/task/create":     s.Wrap(s.HandleTaskCreate),
		"/task/rollback":   s.Wrap(s.HandleTaskRollback),
		"/recovery-plan":   s.Wrap(s.HandleRecoveryPlan),
	}
}

// HandleTaskStatus — POST /task/status. Transitions one task's state.
//
// §3.2 DELIBERATE EXCEPTION to s.Wrap. Pre-§3.2 contract:
//   - ErrNotFound (or *DomainError{Code: CodeNotFound}) → 400 (client mistake)
//   - Any other error → 422 (semantic: unknown state value)
// The "else → 422" branch is non-standard (all other shells use 500).
// server.mapStatus falls through to 500 for unknown-typed errors, so
// HandleTaskStatus stays inline and Routes() registers it WITHOUT
// s.Wrap. Wire bytes and status codes identical to pre-§3.2.
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
	if err := s.Svc.Status(r.Context(), req.ID, req.Status, state.Schema); err != nil {
		s.Metrics.IncErr()
		if errors.Is(err, core.ErrNotFound) {
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
// §3.2 — error-returning handler.
func (s *HTTPService) HandleTaskExecutable(w http.ResponseWriter, r *http.Request) error {
	goals := r.URL.Query().Get("goal_id")
	state := s.Refs.Load()
	tasks, err := s.Svc.Executable(r.Context(), goals, state.Schema)
	if err != nil {
		return err
	}
	s.Metrics.IncTaskExec()
	httputil.WriteJSON(w, http.StatusOK, core.TaskExecutableResponse{Tasks: tasks})
	return nil
}

// HandleTaskList — POST /task/list.
//
// §3.2 — error-returning handler.
func (s *HTTPService) HandleTaskList(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskListRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return nil
	}
	state := s.Refs.Load()
	tasks, err := s.Svc.List(r.Context(), req.Status, req.GoalID, state.Schema)
	if err != nil {
		return err
	}
	s.Metrics.IncTaskList()
	httputil.WriteJSON(w, http.StatusOK, core.TaskExecutableResponse{Tasks: tasks})
	return nil
}

// HandleTaskShow — POST /task/show.
//
// §3.2 — error-returning handler. The pre-§3.2 inline ErrNotFound →
// 400 mapping is now handled by server.mapStatus' CodeNotFound
// branch (and sentinel ErrNotFound branch); other errors fall
// through to 500 default.
func (s *HTTPService) HandleTaskShow(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskShowRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return nil
	}
	if req.ID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "id required")
		return nil
	}
	state := s.Refs.Load()
	entity, blocked, recovers, err := s.Svc.Show(r.Context(), req.ID, state.Schema)
	if err != nil {
		return err
	}
	s.Metrics.IncTaskShow()
	httputil.WriteJSON(w, http.StatusOK, core.TaskShowResponse{Entity: entity, BlockedBy: blocked, RecoversVia: recovers})
	return nil
}

// HandleTaskDep — POST /task/dep.
//
// §3.2 — error-returning handler. Pre-validation against schema,
// relation-type validity, and schema-conflict stays inline (they
// are transport-level gates, not domain errors).
func (s *HTTPService) HandleTaskDep(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskDepRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return nil
	}
	if req.SourceID == "" || req.TargetID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "source_id, target_id required")
		return nil
	}
	state := s.Refs.Load()
	rel := req.RelationType
	if rel == "" {
		rel = state.Schema.RelationBlocking
	}
	if !state.ValidRelationTypes[rel] {
		httputil.WriteError(w, http.StatusUnprocessableEntity, "unknown relation_type: "+rel)
		return nil
	}
	if err := s.Svc.Dep(r.Context(), req.SourceID, req.TargetID, rel, req.Add); err != nil {
		return err
	}
	s.Metrics.IncTaskDep()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}

// HandleTaskRollback — POST /task/rollback.
//
// §3.2 — error-returning handler.
func (s *HTTPService) HandleTaskRollback(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskRollbackRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return nil
	}
	if req.ID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "id required")
		return nil
	}
	rollbackID, err := s.Svc.Rollback(r.Context(), req.ID, s.Refs.Load().Schema)
	if err != nil {
		return err
	}
	s.Metrics.IncTaskRollback()
	httputil.WriteJSON(w, http.StatusOK, core.TaskRollbackResponse{RollbackTaskID: rollbackID})
	return nil
}

// HandleTaskTree — POST /task/tree.
//
// §3.2 — error-returning handler.
func (s *HTTPService) HandleTaskTree(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskTreeRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return nil
	}
	tree, err := s.Svc.Tree(r.Context(), req.GoalID, s.Refs.Load().Schema)
	if err != nil {
		return err
	}
	s.Metrics.IncTaskTree()
	httputil.WriteJSON(w, http.StatusOK, core.TaskTreeResponse{Tree: tree})
	return nil
}

// HandleTaskCreate — POST /task/create.
//
// §3.2 — error-returning handler. The ErrInvalidInput → 422 mapping
// is now handled by server.mapStatus' CodeInvalidInput (and
// ErrInvalidInput sentinel) branch; other errors fall through to
// 500 default.
func (s *HTTPService) HandleTaskCreate(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.TaskCreateRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return nil
	}
	if req.Content == "" {
		httputil.WriteError(w, http.StatusBadRequest, "content required")
		return nil
	}
	if req.ID == "" {
		req.ID = core.NewTaskID()
	}
	state := s.Refs.Load()
	newID, err := s.Svc.Create(r.Context(), req.ID, req.Content, req.ContextIDs, state.Schema)
	if err != nil {
		return err
	}
	s.Metrics.IncTaskCreate()
	httputil.WriteJSON(w, http.StatusOK, core.TaskCreateResponse{ID: newID, Status: "ok"})
	return nil
}

// HandleRecoveryPlan — GET /recovery-plan[?id=X].
//
// §3.2 — error-returning handler.
func (s *HTTPService) HandleRecoveryPlan(w http.ResponseWriter, r *http.Request) error {
	id := r.URL.Query().Get("id")
	if id == "" {
		httputil.WriteError(w, http.StatusBadRequest, "id required")
		return nil
	}
	plan, err := s.Svc.RecoveryPlan(r.Context(), id, s.Refs.Load().Schema)
	if err != nil {
		return err
	}
	httputil.WriteJSON(w, http.StatusOK, plan)
	return nil
}
