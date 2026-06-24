// Package memory hosts the write-side HTTP service: store, ingest, edge, timeline.
//
// MemoryService owns the IngestionWorker — the Server shell has no business
// knowing what ingestion does. OnStateChange is called by Server.ReloadState
// whenever a SIGHUP swaps the atomic State; the worker gets the new schema so
// it can validate input dialogs against it.
package memory

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/ingestion"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Service handles write-side endpoints.
type Service struct {
	DB       *sql.DB
	VI       core.VectorIndex
	Embedder core.Embedder
	Worker   *ingestion.IngestionWorker
	Refs     *serverstate.Ref
}

// New constructs a Service.
func New(db *sql.DB, vi core.VectorIndex, embedder core.Embedder, worker *ingestion.IngestionWorker, refs *serverstate.Ref) *Service {
	return &Service{DB: db, VI: vi, Embedder: embedder, Worker: worker, Refs: refs}
}

// Routes returns the URL → handler mapping for this service.
func (s *Service) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/store":    s.HandleStore,
		"/ingest":   s.HandleIngest,
		"/edge":     s.HandleEdge,
		"/timeline": s.HandleTimeline,
	}
}

// OnStateChange propagates the new schema to the ingestion worker.
// Called by Server.ReloadState after the atomic Ref swap.
func (s *Service) OnStateChange(state *serverstate.State) {
	s.Worker.ReloadSchema(state.Schema)
}

// rejectSchemaConflict writes the canonical 409 Schema-Conflict envelope
// and returns true if a SIGHUP swapped the global config since the
// handler captured state.Generation at request start. The caller pattern
// is:
//
//	if s.rejectSchemaConflict(w, state.Generation) { return }
//
// Centralising the JSON code string + metrics increment + status ensures
// HandleStore + HandleEdge (and any future handler that adopts the same
// guard) cannot drift on the envelope. Bumps IncSchemaConflict (not
// IncErr) so a SIGHUP-burst of rejected writes doesn't pollute the
// operator's error-rate dashboard — schema-concurrency 409s are healthy.
func (s *Service) rejectSchemaConflict(w http.ResponseWriter, gen uint64) bool {
	if !s.Refs.IsStale(gen) {
		return false
	}
	metrics.IncSchemaConflict()
	httputil.WriteErrorWithCode(w, http.StatusConflict,
		"schema changed during request; retry",
		"schema_conflict", "")
	return true
}

func (s *Service) HandleStore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.StoreRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.ID == "" || req.Category == "" || req.Content == "" {
		httputil.WriteError(w, http.StatusBadRequest, "id, category, content required")
		return
	}
	state := s.Refs.Load()
	if !state.ValidCategories[req.Category] {
		httputil.WriteError(w, http.StatusUnprocessableEntity, fmt.Sprintf("unknown category: %s", req.Category))
		return
	}
	// Cross-state tx guard: see Service.rejectSchemaConflict.
	if s.rejectSchemaConflict(w, state.Generation) {
		return
	}
	entity := core.Entity{ID: req.ID, Category: req.Category, Content: req.Content, Embedding: req.Embedding}
	if err := store.StoreEntityWithEmbedding(s.DB, s.VI, state.Schema, entity); err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	vector.AutoLinkEdges(r.Context(), s.DB, s.VI, s.Embedder, req.ID, entity.Embedding)
	metrics.IncStore()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Service) HandleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.IngestRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.Dialog == "" {
		httputil.WriteError(w, http.StatusBadRequest, "dialog required")
		return
	}
	if err := s.Worker.ProcessDialog(r.Context(), req.Dialog); err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncIngest()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Service) HandleEdge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	var req core.EdgeRequest
	if code, field, msg, ok := httputil.DecodeStrict(r.Body, &req); !ok {
		httputil.WriteErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}
	if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
		httputil.WriteError(w, http.StatusBadRequest, "source_id, target_id, relation_type required")
		return
	}
	state := s.Refs.Load()
	if !state.ValidRelationTypes[req.RelationType] {
		httputil.WriteError(w, http.StatusUnprocessableEntity, fmt.Sprintf("unknown relation_type: %s", req.RelationType))
		return
	}
	// Cross-state tx guard: see Service.rejectSchemaConflict.
	if s.rejectSchemaConflict(w, state.Generation) {
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
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	metrics.IncEdge()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleTimeline returns recently created entities (raw SQL — not agent-derived).
type TimelineEntry struct {
	ID, Category, Content                         string
	CreatedAt                                     *time.Time
	Source, SourceType, ConversationID, MessageID string
}

func (s *Service) HandleTimeline(w http.ResponseWriter, r *http.Request) {
	limit := httputil.ParseIntParam(r, "limit", 50)
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, category, content, created_at, source, source_type, conversation_id, message_id FROM entities WHERE archived = 0 AND created_at IS NOT NULL ORDER BY created_at DESC LIMIT ?`,
		limit)
	if err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var entries []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		var createdAt sql.NullTime
		var source, sourceType, convID, msgID sql.NullString
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &createdAt, &source, &sourceType, &convID, &msgID); err != nil {
			metrics.IncErr()
			httputil.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
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
	if err := rows.Err(); err != nil {
		metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entries == nil {
		entries = []TimelineEntry{}
	}
	httputil.WriteJSON(w, http.StatusOK, entries)
}
