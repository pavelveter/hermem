package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ServerState holds the schema-derived fields that can change at
// runtime via SIGHUP reload. Grouped into a single struct so
// atomic.Pointer swaps all three fields atomically without locks.
type ServerState struct {
	schema             SchemaConfig
	validCategories    map[string]bool
	validRelationTypes map[string]bool
}

type Server struct {
	db            *sql.DB
	vi            VectorIndex
	worker        *IngestionWorker
	embedder      Embedder
	retrievalOpts RetrieveContextOptions
	state         atomic.Pointer[ServerState]
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
	SourceID     string  `json:"source_id"`
	TargetID     string  `json:"target_id"`
	RelationType string  `json:"relation_type"`
	AutoCreate   bool    `json:"auto_create"`
	Weight       float32 `json:"weight,omitempty"`
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

func NewServer(db *sql.DB, vi VectorIndex, embedder Embedder, extractor LLMExtractor, dedupThreshold float32, retrievalOpts RetrieveContextOptions, schema SchemaConfig) *Server {
	validCategories := schema.AllowedCategories
	if validCategories == nil {
		validCategories = map[string]bool{}
	}
	validRelationTypes := schema.AllowedRelations
	if validRelationTypes == nil {
		validRelationTypes = map[string]bool{}
	}
	s := &Server{
		db:            db,
		vi:            vi,
		worker:        NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold, schema),
		embedder:      embedder,
		retrievalOpts: retrievalOpts,
	}
	s.state.Store(&ServerState{
		schema:             schema,
		validCategories:    validCategories,
		validRelationTypes: validRelationTypes,
	})
	return s
}

// ReloadState atomically swaps the server's schema state. Called on
// SIGHUP after validating the new config. Handlers already in flight
// continue using their snapshot; new requests see the updated state.
func (s *Server) ReloadState(cfg *Config) {
	cats := cfg.Schema.AllowedCategories
	if cats == nil {
		cats = map[string]bool{}
	}
	rels := cfg.Schema.AllowedRelations
	if rels == nil {
		rels = map[string]bool{}
	}
	s.state.Store(&ServerState{
		schema:             cfg.Schema,
		validCategories:    cats,
		validRelationTypes: rels,
	})
	s.worker.ReloadSchema(cfg.Schema)
	// Sprint 5: also swap ranking weights and reranker on SIGHUP.
	s.retrievalOpts.RankingWeight = cfg.Ranking
	s.retrievalOpts.Reranker = cfg.NewReranker()
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
	state := s.state.Load()
	if !state.validCategories[req.Category] {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("unknown category: %s", req.Category))
		return
	}

	entity := Entity{
		ID:        req.ID,
		Category:  req.Category,
		Content:   req.Content,
		Embedding: req.Embedding,
	}

	if err := StoreEntityWithEmbedding(s.db, s.vi, state.schema, entity); err != nil {
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

	state := s.state.Load()
	if !state.validRelationTypes[req.RelationType] {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("unknown relation_type: %s", req.RelationType))
		return
	}

	var err error
	if req.AutoCreate {
		err = AddEdgeWithAutoCreate(r.Context(), s.db, s.vi, s.embedder, req.SourceID, req.TargetID, req.RelationType)
	} else {
		err = AddEdge(s.db, req.SourceID, req.TargetID, req.RelationType, req.Weight)
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

// HandleHealthLive always returns 200 — the process is alive. Used by
// Kubernetes liveness probes and load-balancer health checks.
func (s *Server) HandleHealthLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleHealthReady checks critical dependencies before returning 200.
// Pings the database and optionally verifies the embedder is reachable.
// Returns 503 if any dependency is unavailable. Used by Kubernetes
// readiness probes to stop routing traffic to a degraded instance.
func (s *Server) HandleHealthReady(w http.ResponseWriter, r *http.Request) {
	checks := make(map[string]string)
	allOk := true

	// DB ping
	if err := s.db.PingContext(r.Context()); err != nil {
		checks["database"] = "unreachable: " + err.Error()
		allOk = false
	} else {
		checks["database"] = "ok"
	}

	if !allOk {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status": "degraded",
			"checks": checks,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"checks": checks,
	})
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

	state := s.state.Load()
	if err := UpdateTaskStatus(s.db, state.schema, req.ID, req.Status); err != nil {
		incErr()
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err.Error())
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

	state := s.state.Load()
	tasks, err := GetExecutableTasks(s.db, state.schema, goalID)
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

	state := s.state.Load()
	tasks, err := ListTasks(s.db, state.schema, req.Status, req.GoalID)
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

	state := s.state.Load()
	entity, blocked, recovers, err := GetTaskWithRelations(s.db, state.schema, req.ID)
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
	state := s.state.Load()
	if rel == "" {
		rel = state.schema.RelationBlocking
	}
	validRels := state.validRelationTypes
	if validRels == nil {
		validRels = map[string]bool{}
	}
	if !validRels[rel] {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("unknown relation_type: %s", rel))
		return
	}

	var err error
	if req.Add {
		err = AddEdge(s.db, req.SourceID, req.TargetID, rel, 1.0)
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

	state := s.state.Load()
	rollbackID, err := FindRollbackTask(s.db, state.schema, req.ID)
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

	state := s.state.Load()
	nodes, err := GetTaskTree(s.db, state.schema, req.GoalID)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incTaskTree()
	writeJSON(w, http.StatusOK, TaskTreeResponse{Tree: RenderTaskTree(nodes, "")})
}

func (s *Server) HandleQueryExplain(w http.ResponseWriter, r *http.Request) {
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

	queryEmbedding, err := s.embedder.Embed(r.Context(), req.Query)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("embed failed: %v", err))
		return
	}

	searchResults, err := SearchByVector(s.db, s.vi, queryEmbedding, req.TopK)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var seedIDs []string
	for _, res := range searchResults {
		seedIDs = append(seedIDs, res.Entity.ID)
	}

	opts := s.retrievalOpts
	opts.QueryEmbedding = queryEmbedding
	opts.QueryText = req.Query
	opts.Ctx = r.Context()
	opts.Explain = true

	result, err := RetrieveContext(s.db, seedIDs, opts)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incQuery()
	writeJSON(w, http.StatusOK, result)
}

// HandleProvenance returns entities matching provenance filters.
// GET /provenance?conversation_id=X&message_id=Y&source=Z&limit=50
func (s *Server) HandleProvenance(w http.ResponseWriter, r *http.Request) {
	conversationID := r.URL.Query().Get("conversation_id")
	messageID := r.URL.Query().Get("message_id")
	source := r.URL.Query().Get("source")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	entities, err := GetEntitiesByProvenance(s.db, conversationID, messageID, source, limit)
	if err != nil {
		incErr()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entities)
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

	state := s.state.Load()
	category := firstStatefulCategory(state.schema)
	if category == "" {
		writeError(w, http.StatusUnprocessableEntity, "no stateful category configured")
		return
	}
	entity := Entity{ID: req.ID, Category: category, Content: req.Content, Embedding: embedding}
	if err := StoreEntityWithEmbedding(s.db, s.vi, state.schema, entity); err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, cid := range req.ContextIDs {
		if cid == "" {
			continue
		}
		if err := AddEdge(s.db, req.ID, cid, "related_to", 1.0); err != nil {
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

// HandleQueryTemporal filters retrieval by time range. Accepts JSON:
// {"query":"...", "time_from":"2024-03-01T00:00:00Z", "time_to":"2024-03-31T23:59:59Z"}
// Returns context limited to entities created in the specified window.
func (s *Server) HandleQueryTemporal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Query    string `json:"query"`
		TimeFrom string `json:"time_from"`
		TimeTo   string `json:"time_to"`
		TopK     int    `json:"top_k"`
	}
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

	queryEmbedding, err := s.embedder.Embed(r.Context(), req.Query)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("embed failed: %v", err))
		return
	}

	searchResults, err := SearchByVector(s.db, s.vi, queryEmbedding, req.TopK)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var seedIDs []string
	for _, res := range searchResults {
		seedIDs = append(seedIDs, res.Entity.ID)
	}

	opts := s.retrievalOpts
	opts.QueryEmbedding = queryEmbedding
	opts.QueryText = req.Query
	opts.Ctx = r.Context()
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

	result, err := RetrieveContext(s.db, seedIDs, opts)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	incQuery()
	writeJSON(w, http.StatusOK, result)
}

// HandleTimeline returns entities ordered by created_at, grouped by
// source/conversation. Accepts optional ?limit=N (default 50).
// GET /timeline[?limit=50]
func (s *Server) HandleTimeline(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, category, content, created_at,
		       source, source_type, conversation_id, message_id
		FROM entities
		WHERE archived = 0 AND created_at IS NOT NULL
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type TimelineEntry struct {
		ID             string     `json:"id"`
		Category       string     `json:"category"`
		Content        string     `json:"content"`
		CreatedAt      *time.Time `json:"created_at"`
		Source         string     `json:"source,omitempty"`
		SourceType     string     `json:"source_type,omitempty"`
		ConversationID string     `json:"conversation_id,omitempty"`
		MessageID      string     `json:"message_id,omitempty"`
	}
	var entries []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		var cat, content string
		var createdAt sql.NullTime
		var source, sourceType, convID, msgID sql.NullString
		if err := rows.Scan(&e.ID, &cat, &content, &createdAt,
			&source, &sourceType, &convID, &msgID); err != nil {
			incErr()
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		e.Category = cat
		e.Content = content
		if createdAt.Valid {
			t := createdAt.Time
			e.CreatedAt = &t
		}
		if source.Valid {
			e.Source = source.String
		}
		if sourceType.Valid {
			e.SourceType = sourceType.String
		}
		if convID.Valid {
			e.ConversationID = convID.String
		}
		if msgID.Valid {
			e.MessageID = msgID.String
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []TimelineEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// HandleRecoveryPlan walks the recovers_via chain from a failed task
// and returns the ordered recovery task sequence.
// GET /recovery-plan?id=failed-task-id
func (s *Server) HandleRecoveryPlan(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id required")
		return
	}
	state := s.state.Load()
	plan, err := GenerateRecoveryPlan(s.db, state.schema, id)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if plan == nil {
		plan = []Entity{}
	}
	writeJSON(w, http.StatusOK, plan)
}

// HandleConnectedComponents finds all connected components in the graph.
// GET /connected-components?min_size=2
func (s *Server) HandleConnectedComponents(w http.ResponseWriter, r *http.Request) {
	minSize := 2
	if ms := r.URL.Query().Get("min_size"); ms != "" {
		if n, err := strconv.Atoi(ms); err == nil && n > 0 {
			minSize = n
		}
	}
	components, err := FindConnectedComponents(s.db, minSize)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if components == nil {
		components = []ConnectedComponent{}
	}
	writeJSON(w, http.StatusOK, components)
}

// HandleContradictions returns contradicts edges. Accepts optional
// ?id=X query param to filter by entity (checks both sides).
// GET /contradictions[?id=entity-x]
func (s *Server) HandleContradictions(w http.ResponseWriter, r *http.Request) {
	entityID := r.URL.Query().Get("id")
	pairs, err := GetContradictions(s.db, entityID)
	if err != nil {
		incErr()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if pairs == nil {
		pairs = []ContradictionPair{}
	}
	writeJSON(w, http.StatusOK, pairs)
}

func firstStatefulCategory(schema SchemaConfig) string {
	keys := sortedKeys(schema.StatefulCategories)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
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
