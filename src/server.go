package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	db            *sql.DB
	vi            VectorIndex
	worker        *IngestionWorker
	embedder      Embedder
	retrievalOpts RetrieveContextOptions
	// validRelationTypes is the merged relation-type allowlist
	// (defaults + Config.ExtraRelationTypes), shared with the
	// extractor so HTTP-request validation matches the runtime
	// filter the ingester applies. nil maps count as "no entries
	// allowed" — production callers always pass a non-nil map.
	validRelationTypes map[string]bool
}

type StoreRequest struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
}

type SearchRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

type RetrieveRequest struct {
	SeedIDs  []string `json:"seed_ids"`
	MaxDepth int      `json:"max_depth"`
}

type IngestRequest struct {
	Dialog string `json:"dialog"`
}

type EdgeRequest struct {
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
	AutoCreate   bool   `json:"auto_create"`
}

type TaskStatusRequest struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type TaskExecutableResponse struct {
	Tasks []Entity `json:"tasks"`
}

type TaskListRequest struct {
	Status string `json:"status"`
	GoalID string `json:"goal_id"`
}

type TaskShowRequest struct {
	ID string `json:"id"`
}

type TaskShowResponse struct {
	Entity      Entity `json:"entity"`
	BlockedBy   []Edge `json:"blocked_by"`
	RecoversVia []Edge `json:"recovers_via"`
}

type TaskDepRequest struct {
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
	Add          bool   `json:"add"`
}

type TaskRollbackRequest struct {
	ID string `json:"id"`
}

type TaskRollbackResponse struct {
	RollbackTaskID string `json:"rollback_task_id"`
}

type TaskTreeRequest struct {
	GoalID string `json:"goal_id"`
}

type TaskTreeResponse struct {
	Tree string `json:"tree"`
}

type TaskCreateRequest struct {
	ID         string   `json:"id"`
	Content    string   `json:"content"`
	ContextIDs []string `json:"context_ids,omitempty"`
}

type TaskCreateResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// ErrorResponse carries a human message plus an optional (code, field)
// pair so clients can route the rejection without parsing prose. Both
// optional fields are omitempty so non-strict errors (method-not-allowed,
// missing-required-field) stay shape-compatible with the pre-PR7 wire
// contract.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
	Field string `json:"field,omitempty"`
}

func NewServer(db *sql.DB, vi VectorIndex, embedder Embedder, extractor LLMExtractor, dedupThreshold float32, retrievalOpts RetrieveContextOptions, validRelationTypes map[string]bool) *Server {
	if validRelationTypes == nil {
		validRelationTypes = map[string]bool{}
	}
	return &Server{
		db:                 db,
		vi:                 vi,
		worker:             NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold),
		embedder:           embedder,
		retrievalOpts:      retrievalOpts,
		validRelationTypes: validRelationTypes,
	}
}

// decodeStrict parses JSON from an io.Reader into dst while rejecting
// unknown fields via encoding/json.DisallowUnknownFields. On any
// failure it returns a (code, field, msg, ok) tuple that the caller
// forwards to writeErrorWithCode so clients get a structured rejection
// distinguishing empty_body / unknown_field / invalid_type / bad_json.
// The bool is false on any decode error.
//
// Used uniformly by the HTTP handlers and the CLI JSON-stdin parsers
// (main.go wraps stdin in bytes.NewReader) so the wire contract is
// identical end-to-end: an unknown field, an empty body, or a
// wrong-typed field yields the same response shape in either surface.
func decodeStrict(r io.Reader, dst interface{}) (code, field, msg string, ok bool) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
		// Strict-mode invariant: exactly one JSON value per request.
		// dec.More() returns true if more bytes remain in the buffer
		// after a successful Decode, which catches `{...}{...}` and
		// `{...} garbage` — both of which would otherwise silently
		// consume only the first object.
		if dec.More() {
			return "trailing_data", "", "trailing data after JSON value", false
		}
		return "", "", "", true
	}
	if errors.Is(err, io.EOF) {
		return "empty_body", "", "request body is empty", false
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return "invalid_type", typeErr.Field,
			fmt.Sprintf("invalid type for field %q (got %s, want %s)",
				typeErr.Field, typeErr.Value, typeErr.Type), false
	}
	if strings.HasPrefix(err.Error(), "json: unknown field") {
		// err.Error() looks like: json: unknown field "foo"
		rest := strings.TrimPrefix(err.Error(), "json: unknown field ")
		fieldName := strings.Trim(rest, "\"")
		return "unknown_field", fieldName, "unknown field: " + fieldName, false
	}
	return "bad_json", "", "invalid json: " + err.Error(), false
}

func (s *Server) HandleStore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req StoreRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.ID == "" || req.Category == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, "id, category, content required")
		return
	}

	entity := Entity{
		ID:        req.ID,
		Category:  req.Category,
		Content:   req.Content,
		Embedding: req.Embedding,
	}

	if err := StoreEntityWithEmbedding(s.db, s.vi, entity); err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := AutoLinkEdges(r.Context(), s.db, s.vi, s.embedder, req.ID, entity.Embedding); err != nil {
		slog.Warn("auto-link failed", withReqID(r.Context(), "event", "auto_link_failed", "id", req.ID, "error", err)...)
	}

	incStore()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req SearchRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query required")
		return
	}

	if req.TopK <= 0 {
		req.TopK = 5
	}

	embedding, err := s.embedder.Embed(r.Context(), req.Query)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("embed failed: %v", err))
		return
	}

	results, err := SearchByVector(s.db, s.vi, embedding, req.TopK)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incSearch()
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) HandleRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req RetrieveRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if len(req.SeedIDs) == 0 {
		writeError(w, http.StatusBadRequest, "seed_ids required")
		return
	}

	if req.MaxDepth <= 0 {
		req.MaxDepth = 2
	}

	opts := s.retrievalOpts
	opts.MaxDepth = req.MaxDepth
	opts.Ctx = r.Context()
	result, err := RetrieveContext(s.db, req.SeedIDs, opts)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incRetrieve()
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) HandleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req IngestRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.Dialog == "" {
		writeError(w, http.StatusBadRequest, "dialog required")
		return
	}

	if err := s.worker.ProcessDialog(r.Context(), req.Dialog); err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incIngest()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req SearchRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query required")
		return
	}

	if req.TopK <= 0 {
		req.TopK = 3
	}

	context, err := GenerateResponse(r.Context(), s.db, s.vi, s.embedder, s.retrievalOpts, req.Query)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incQuery()
	writeJSON(w, http.StatusOK, map[string]string{"context": context})
}

func (s *Server) HandleEdge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req EdgeRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
		writeError(w, http.StatusBadRequest, "source_id, target_id, relation_type required")
		return
	}

	if !s.validRelationTypes[req.RelationType] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid relation_type: %s", req.RelationType))
		return
	}

	var err error
	if req.AutoCreate {
		err = AddEdgeWithAutoCreate(r.Context(), s.db, s.vi, s.embedder, req.SourceID, req.TargetID, req.RelationType)
	} else {
		err = AddEdge(s.db, req.SourceID, req.TargetID, req.RelationType)
	}
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incEdge()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleTaskStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req TaskStatusRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.ID == "" || req.Status == "" {
		writeError(w, http.StatusBadRequest, "id, status required")
		return
	}

	if err := UpdateTaskStatus(s.db, req.ID, req.Status); err != nil {
		incErr()
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	incTaskStatus()
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) HandleTaskExecutable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	goalID := r.URL.Query().Get("goal_id")

	tasks, err := GetExecutableTasks(s.db, goalID)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if tasks == nil {
		tasks = []Entity{}
	}

	incTaskExecutable()
	writeJSON(w, http.StatusOK, TaskExecutableResponse{Tasks: tasks})
}

func (s *Server) HandleTaskList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req TaskListRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	tasks, err := ListTasks(s.db, req.Status, req.GoalID)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if tasks == nil {
		tasks = []Entity{}
	}

	incTaskList()
	writeJSON(w, http.StatusOK, TaskExecutableResponse{Tasks: tasks})
}

func (s *Server) HandleTaskShow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req TaskShowRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id required")
		return
	}

	entity, blocked, recovers, err := GetTaskWithRelations(s.db, req.ID)
	if err != nil {
		incErr()
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incTaskShow()
	writeJSON(w, http.StatusOK, TaskShowResponse{Entity: entity, BlockedBy: blocked, RecoversVia: recovers})
}

func (s *Server) HandleTaskDep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req TaskDepRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.SourceID == "" || req.TargetID == "" {
		writeError(w, http.StatusBadRequest, "source_id, target_id required")
		return
	}

	rel := req.RelationType
	if rel == "" {
		rel = "blocked_by"
	}
	validRels := s.validRelationTypes
	if validRels == nil {
		validRels = map[string]bool{}
	}
	if !validRels[rel] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid relation_type: %s", rel))
		return
	}

	var err error
	if req.Add {
		err = AddEdge(s.db, req.SourceID, req.TargetID, rel)
	} else {
		err = DeleteEdge(s.db, req.SourceID, req.TargetID, rel)
	}
	if err != nil {
		incErr()
		if strings.Contains(err.Error(), "no such") || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "UNIQUE constraint") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	incTaskDep()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleTaskRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req TaskRollbackRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "id required")
		return
	}

	rollbackID, err := FindRollbackTask(s.db, req.ID)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incTaskRollback()
	writeJSON(w, http.StatusOK, TaskRollbackResponse{RollbackTaskID: rollbackID})
}

func (s *Server) HandleTaskTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req TaskTreeRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	nodes, err := GetTaskTree(s.db, req.GoalID)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incTaskTree()
	writeJSON(w, http.StatusOK, TaskTreeResponse{Tree: RenderTaskTree(nodes, "")})
}

func (s *Server) HandleTaskCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req TaskCreateRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content required")
		return
	}

	if req.ID == "" {
		req.ID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	}

	embedding, err := s.embedder.Embed(r.Context(), req.Content)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	entity := Entity{ID: req.ID, Category: "task", Content: req.Content, Embedding: embedding}
	if err := StoreEntityWithEmbedding(s.db, s.vi, entity); err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, cid := range req.ContextIDs {
		if cid == "" {
			continue
		}
		if err := AddEdge(s.db, req.ID, cid, "related_to"); err != nil {
			slog.Error("failed to add context edge", "err", err, "from", req.ID, "to", cid)
		}
	}

	if err := AutoLinkEdges(r.Context(), s.db, s.vi, s.embedder, req.ID, embedding); err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incTaskCreate()
	writeJSON(w, http.StatusOK, TaskCreateResponse{ID: req.ID, Status: "ok"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError carries a single ErrorResponse. Both Code and Field default
// to "" and are omitted via omitempty, so this remains wire-compatible
// with the pre-PR7 response shape for callers that never trigger a
// strict-decode rejection.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

// writeErrorWithCode pairs the human message with the structured code
// + field pair, used by strictDecode rejections so clients can
// programmatically distinguish empty_body, unknown_field, invalid_type,
// and bad_json without parsing prose.
func writeErrorWithCode(w http.ResponseWriter, status int, msg, code, field string) {
	writeJSON(w, status, ErrorResponse{
		Error: msg,
		Code:  code,
		Field: field,
	})
}
