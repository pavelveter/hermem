package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pavelveter/hermem/src/internal/algo"
	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// --- HTTP Handlers (extracted from server.go for clarity) ---

func (s *Server) HandleStore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.StoreRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.ID == "" || req.Category == "" || req.Content == "" {
		WriteError(w, http.StatusBadRequest, "id, category, content required")
		return
	}
	state := s.State.Load()
	if !state.ValidCategories[req.Category] {
		WriteError(w, http.StatusUnprocessableEntity, fmt.Sprintf("unknown category: %s", req.Category))
		return
	}
	entity := core.Entity{ID: req.ID, Category: req.Category, Content: req.Content, Embedding: req.Embedding}
	if err := store.StoreEntityWithEmbedding(s.DB, s.VI, state.Schema, entity); err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	vector.AutoLinkEdges(r.Context(), s.DB, s.VI, s.Embedder, req.ID, entity.Embedding)
	metrics.IncStore()
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.SearchRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.Query == "" {
		WriteError(w, http.StatusBadRequest, "query required")
		return
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	embedding, err := s.Embedder.Embed(r.Context(), req.Query)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("embed failed: %v", err))
		return
	}
	results, err := vector.SearchByVector(s.DB, s.VI, embedding, req.TopK)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncSearch()
	WriteJSON(w, http.StatusOK, results)
}

func (s *Server) HandleRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.RetrieveRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if len(req.SeedIDs) == 0 {
		WriteError(w, http.StatusBadRequest, "seed_ids required")
		return
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = 2
	}
	opts := s.RetrievalOpts
	opts.MaxDepth = req.MaxDepth
	opts.Ctx = r.Context()
	result, err := retrieval.RetrieveContext(s.DB, req.SeedIDs, opts)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncRetrieve()
	WriteJSON(w, http.StatusOK, result)
}

func (s *Server) HandleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.IngestRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.Dialog == "" {
		WriteError(w, http.StatusBadRequest, "dialog required")
		return
	}
	if err := s.Worker.ProcessDialog(r.Context(), req.Dialog); err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncIngest()
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Query    string `json:"query"`
		MaxDepth int    `json:"max_depth,omitempty"`
	}
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.Query == "" {
		WriteError(w, http.StatusUnprocessableEntity, "query is required")
		return
	}
	opts := s.RetrievalOpts
	if req.MaxDepth > 0 {
		opts.MaxDepth = req.MaxDepth
	}
	out, err := retrieval.GenerateResponse(r.Context(), s.DB, s.VI, s.Embedder, opts, req.Query)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, "response generation failed: "+err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"response": out})
}

func (s *Server) HandleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.SearchRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.Query == "" {
		WriteError(w, http.StatusBadRequest, "query required")
		return
	}
	if req.TopK <= 0 {
		req.TopK = 3
	}
	embedding, err := s.Embedder.Embed(r.Context(), req.Query)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	results, _ := vector.SearchByVector(s.DB, s.VI, embedding, req.TopK)
	seedIDs := make([]string, 0, len(results))
	for _, res := range results {
		seedIDs = append(seedIDs, res.Entity.ID)
	}
	opts := s.RetrievalOpts
	opts.QueryEmbedding = embedding
	opts.QueryText = req.Query
	opts.Ctx = r.Context()
	ctxResult, err := retrieval.RetrieveContext(s.DB, seedIDs, opts)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncQuery()
	WriteJSON(w, http.StatusOK, map[string]string{"context": retrieval.FormatContextMarkdown(ctxResult)})
}

func (s *Server) HandleEdge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.EdgeRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
		WriteError(w, http.StatusBadRequest, "source_id, target_id, relation_type required")
		return
	}
	state := s.State.Load()
	if !state.ValidRelationTypes[req.RelationType] {
		WriteError(w, http.StatusUnprocessableEntity, fmt.Sprintf("unknown relation_type: %s", req.RelationType))
		return
	}
	var err error
	if req.AutoCreate {
		err = vector.AddEdgeWithAutoCreate(r.Context(), s.DB, s.VI, s.Embedder, req.SourceID, req.TargetID, req.RelationType)
	} else {
		err = store.AddEdge(s.DB, req.SourceID, req.TargetID, req.RelationType, req.Weight)
	}
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncEdge()
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (s *Server) HandleHealthLive(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleHealthReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{"database": "ok"}
	if err := s.DB.PingContext(r.Context()); err != nil {
		checks["database"] = "unreachable: " + err.Error()
		WriteJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"status": "degraded", "checks": checks})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "checks": checks})
}

func (s *Server) HandleTaskStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.TaskStatusRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.ID == "" || req.Status == "" {
		WriteError(w, http.StatusBadRequest, "id, status required")
		return
	}
	if err := store.SetStatus(s.DB, s.State.Load().Schema, req.ID, req.Status); err != nil {
		metrics.IncErr()
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	metrics.IncTaskStatus()
	WriteJSON(w, http.StatusNoContent, nil)
}

func (s *Server) HandleTaskExecutable(w http.ResponseWriter, r *http.Request) {
	goals := r.URL.Query().Get("goal_id")
	state := s.State.Load()
	tasks, err := retrieval.GetExecutableTasks(s.DB, state.Schema, goals)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []core.Entity{}
	}
	metrics.IncTaskExec()
	WriteJSON(w, http.StatusOK, core.TaskExecutableResponse{Tasks: tasks})
}

func (s *Server) HandleTaskList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.TaskListRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	state := s.State.Load()
	tasks, err := store.ListTasks(s.DB, state.Schema, req.Status, req.GoalID)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []core.Entity{}
	}
	metrics.IncTaskList()
	WriteJSON(w, http.StatusOK, core.TaskExecutableResponse{Tasks: tasks})
}

func (s *Server) HandleTaskShow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.TaskShowRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.ID == "" {
		WriteError(w, http.StatusBadRequest, "id required")
		return
	}
	state := s.State.Load()
	entity, blocked, recovers, err := store.GetTaskWithRelations(s.DB, state.Schema, req.ID)
	if err != nil {
		metrics.IncErr()
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncTaskShow()
	WriteJSON(w, http.StatusOK, core.TaskShowResponse{Entity: entity, BlockedBy: blocked, RecoversVia: recovers})
}

func (s *Server) HandleTaskDep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.TaskDepRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.SourceID == "" || req.TargetID == "" {
		WriteError(w, http.StatusBadRequest, "source_id, target_id required")
		return
	}
	rel := req.RelationType
	state := s.State.Load()
	if rel == "" {
		rel = state.Schema.RelationBlocking
	}
	if !state.ValidRelationTypes[rel] {
		WriteError(w, http.StatusUnprocessableEntity, fmt.Sprintf("unknown relation_type: %s", rel))
		return
	}
	if req.Add {
		store.AddEdge(s.DB, req.SourceID, req.TargetID, rel, 1.0)
	} else {
		store.DeleteEdge(s.DB, req.SourceID, req.TargetID, rel)
	}
	metrics.IncTaskDep()
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleTaskRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.TaskRollbackRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.ID == "" {
		WriteError(w, http.StatusBadRequest, "id required")
		return
	}
	rollbackID, err := store.FindRollbackTask(s.DB, s.State.Load().Schema, req.ID)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncTaskRollback()
	WriteJSON(w, http.StatusOK, core.TaskRollbackResponse{RollbackTaskID: rollbackID})
}

func (s *Server) HandleTaskTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.TaskTreeRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	nodes, err := store.GetTaskTree(s.DB, s.State.Load().Schema, req.GoalID)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncTaskTree()
	WriteJSON(w, http.StatusOK, core.TaskTreeResponse{Tree: store.RenderTaskTree(nodes, "")})
}

func (s *Server) HandleTaskCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.TaskCreateRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.Content == "" {
		WriteError(w, http.StatusBadRequest, "content required")
		return
	}
	if req.ID == "" {
		req.ID = core.NewTaskID()
	}
	emb, err := s.Embedder.Embed(r.Context(), req.Content)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	state := s.State.Load()
	cat := config.FirstStatefulCategory(state.Schema)
	if cat == "" {
		WriteError(w, http.StatusUnprocessableEntity, "no stateful category configured")
		return
	}
	entity := core.Entity{ID: req.ID, Category: cat, Content: req.Content, Embedding: emb}
	if err := store.StoreEntityWithEmbedding(s.DB, s.VI, state.Schema, entity); err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, cid := range req.ContextIDs {
		if cid != "" {
			store.AddEdge(s.DB, req.ID, cid, "related_to", 1.0)
		}
	}
	vector.AutoLinkEdges(r.Context(), s.DB, s.VI, s.Embedder, req.ID, emb)
	metrics.IncTaskCreate()
	WriteJSON(w, http.StatusOK, core.TaskCreateResponse{ID: req.ID, Status: "ok"})
}

func (s *Server) HandleQueryExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req core.SearchRequest
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.TopK <= 0 {
		req.TopK = 3
	}
	emb, _ := s.Embedder.Embed(r.Context(), req.Query)
	results, _ := vector.SearchByVector(s.DB, s.VI, emb, req.TopK)
	seedIDs := make([]string, 0, len(results))
	for _, res := range results {
		seedIDs = append(seedIDs, res.Entity.ID)
	}
	opts := s.RetrievalOpts
	opts.QueryEmbedding = emb
	opts.QueryText = req.Query
	opts.Ctx = r.Context()
	opts.Explain = true
	result, err := retrieval.RetrieveContext(s.DB, seedIDs, opts)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncQuery()
	WriteJSON(w, http.StatusOK, result)
}

func (s *Server) HandleProvenance(w http.ResponseWriter, r *http.Request) {
	entities, err := store.GetEntitiesByProvenance(s.DB,
		r.URL.Query().Get("conversation_id"), r.URL.Query().Get("message_id"),
		r.URL.Query().Get("source"), parseIntParam(r, "limit", 50))
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, entities)
}

func (s *Server) HandleRecoveryPlan(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		WriteError(w, http.StatusBadRequest, "id required")
		return
	}
	plan, err := store.GenerateRecoveryPlan(s.DB, s.State.Load().Schema, id)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if plan == nil {
		plan = []core.Entity{}
	}
	WriteJSON(w, http.StatusOK, plan)
}

func (s *Server) HandleConnectedComponents(w http.ResponseWriter, r *http.Request) {
	minSize := parseIntParam(r, "min_size", 2)
	components, err := store.FindConnectedComponents(s.DB, minSize)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if components == nil {
		components = []core.ConnectedComponent{}
	}
	WriteJSON(w, http.StatusOK, components)
}

func (s *Server) HandleCommunities(w http.ResponseWriter, r *http.Request) {
	minSize := parseIntParam(r, "min_size", 2)
	maxIter := parseIntParam(r, "max_iterations", 50)
	if maxIter <= 0 || maxIter > 200 {
		maxIter = 50
	}
	all, globalQ, err := store.DetectCommunities(s.DB, maxIter)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var filtered []core.Community
	for _, c := range all {
		if c.Size >= minSize {
			filtered = append(filtered, c)
		}
	}
	if filtered == nil {
		filtered = []core.Community{}
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"communities": filtered, "global_modularity": globalQ,
		"total_communities": len(all), "filtered_communities": len(filtered),
	})
}

func (s *Server) HandleReEmbed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		BatchSize int    `json:"batch_size"`
		Dim       int    `json:"dim"`
		Model     string `json:"model"`
	}
	if code, field, msg, ok := DecodeStrict(r.Body, &req); !ok {
		WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 50
	}
	if req.Dim <= 0 {
		WriteError(w, http.StatusBadRequest, "dim required")
		return
	}
	result, err := algo.ReEmbedAll(r.Context(), s.DB, s.VI, s.Embedder, req.Dim, req.BatchSize, req.Model)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, result)
}

func (s *Server) HandleContradictions(w http.ResponseWriter, r *http.Request) {
	pairs, err := store.GetContradictions(s.DB, r.URL.Query().Get("id"))
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if pairs == nil {
		pairs = []core.ContradictionPair{}
	}
	WriteJSON(w, http.StatusOK, pairs)
}

func (s *Server) HandleTimeline(w http.ResponseWriter, r *http.Request) {
	limit := parseIntParam(r, "limit", 50)
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id, category, content, created_at, source, source_type, conversation_id, message_id FROM entities WHERE archived = 0 AND created_at IS NOT NULL ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		metrics.IncErr()
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	type TimelineEntry struct {
		ID, Category, Content                         string
		CreatedAt                                     *time.Time
		Source, SourceType, ConversationID, MessageID string
	}
	var entries []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		var createdAt sql.NullTime
		var source, sourceType, convID, msgID sql.NullString
		rows.Scan(&e.ID, &e.Category, &e.Content, &createdAt, &source, &sourceType, &convID, &msgID)
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
	WriteJSON(w, http.StatusOK, entries)
}
