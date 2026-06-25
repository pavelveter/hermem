// Package memory hosts the write-side HTTP transport for the memory
// subsystem. Domain logic lives in src/internal/memory (Service); this
// package owns transport-only concerns: JSON encoding, method checks,
// request-body limits, schema-conflict cross-state guard, and metrics
// increments.
//
// The HTTP shell is intentionally thin: every Handle* method is parse →
// validate → delegate-to-domain → write-envelope. The domain Service
// has no knowledge of http.ResponseWriter or serverstate.Ref.
package memory

import (
	"errors"
	"net/http"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	memdomain "github.com/pavelveter/hermem/src/internal/memory"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService is the transport shell for the write-side domain. Holds
// the domain Service (Mem), the metrics counters (Metrics), the
// serverstate.Ref for schema-conflict checks (Refs), and the
// dedupThreshold forwarded to Mem.Ingest for the LLM extraction
// pipeline. Embedder lives inside Mem — no transport-level duplication.
type HTTPService struct {
	Mem            *memdomain.Service
	Metrics        *metrics.Metrics
	Refs           *serverstate.Ref
	DedupThreshold float32
}

// New constructs an HTTPService.
func New(mem *memdomain.Service, m *metrics.Metrics, refs *serverstate.Ref, dedupThreshold float32) *HTTPService {
	return &HTTPService{Mem: mem, Metrics: m, Refs: refs, DedupThreshold: dedupThreshold}
}

// Routes returns the URL → handler mapping. Wired up by Server in
// src/internal/server/server.go via the per-service Routes() protocol.
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/store":    s.HandleStore,
		"/edge":     s.HandleEdge,
		"/timeline": s.HandleTimeline,
	}
}

// rejectSchemaConflict writes the canonical 409 envelope and returns
// true if the schema generation observed at handler entry differs from
// the live Refs (SIGHUP swapped mid-request). Same contract and
// IncSchemaConflict-vs-IncErr separation as the pre-PHASE-2.1 handler.
func (s *HTTPService) rejectSchemaConflict(w http.ResponseWriter, gen uint64) bool {
	if !s.Refs.IsStale(gen) {
		return false
	}
	s.Metrics.IncSchemaConflict()
	httputil.WriteErrorWithCode(w, http.StatusConflict,
		"schema changed during request; retry",
		"schema_conflict", "")
	return true
}

// isSchemaErr reports whether err is a memdomain.ErrInvalidSchema so
// the handler maps the domain's semantic validation failure to 422 —
// matching the pre-PHASE-2.1 envelope exactly so existing clients and
// tests don't see a status drift.
func isSchemaErr(err error) bool {
	if err == nil {
		return false
	}
	var ise *memdomain.ErrInvalidSchema
	return errors.As(err, &ise)
}

// HandleStore — POST /store. Persists one entity with a caller-supplied
// embedding, then fires the HTTP-only AutoLinkEdges side effect via
// memdomain.Service.StoreAndLink (/store only — CLI /store preserves
// its historical non-linking behaviour).
func (s *HTTPService) HandleStore(w http.ResponseWriter, r *http.Request) {
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
	// Pre-PHASE-2.1 the missing-field check happened inline at the
	// HTTP layer — kept here verbatim so existing /store clients
	// continue to see 400 for malformed bodies. The domain Mem.Store
	// also enforces the same fields as defense-in-depth.
	if req.ID == "" || req.Category == "" || req.Content == "" {
		httputil.WriteError(w, http.StatusBadRequest, "id, category, content required")
		return
	}
	state := s.Refs.Load()
	if !state.ValidCategories[req.Category] {
		httputil.WriteError(w, http.StatusUnprocessableEntity, "unknown category: "+req.Category)
		return
	}
	if s.rejectSchemaConflict(w, state.Generation) {
		return
	}
	if err := s.Mem.StoreAndLink(r.Context(), req, state.Schema); err != nil {
		s.Metrics.IncErr()
		status := http.StatusInternalServerError
		if isSchemaErr(err) {
			status = http.StatusUnprocessableEntity
		}
		httputil.WriteError(w, status, err.Error())
		return
	}
	s.Metrics.IncStore()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleEdge — POST /edge. Persists a single relation edge, optionally
// auto-creating missing endpoint entities (AutoCreate=true).
func (s *HTTPService) HandleEdge(w http.ResponseWriter, r *http.Request) {
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
	// Pre-PHASE-2.1 the missing-field check happened inline at the
	// HTTP layer — kept here verbatim so existing /edge clients
	// continue to see 400 for malformed bodies. The domain Mem.AddEdge
	// also enforces the same fields as defense-in-depth.
	if req.SourceID == "" || req.TargetID == "" || req.RelationType == "" {
		httputil.WriteError(w, http.StatusBadRequest, "source_id, target_id, relation_type required")
		return
	}
	state := s.Refs.Load()
	if !state.ValidRelationTypes[req.RelationType] {
		httputil.WriteError(w, http.StatusUnprocessableEntity, "unknown relation_type: "+req.RelationType)
		return
	}
	if s.rejectSchemaConflict(w, state.Generation) {
		return
	}
	if err := s.Mem.AddEdge(r.Context(), req, state.Schema); err != nil {
		s.Metrics.IncErr()
		status := http.StatusInternalServerError
		if isSchemaErr(err) {
			status = http.StatusUnprocessableEntity
		}
		httputil.WriteError(w, status, err.Error())
		return
	}
	s.Metrics.IncEdge()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleTimeline — GET /timeline[?limit=N]. Returns the N most-recently
// created entities (raw SQL — not agent-derived).
func (s *HTTPService) HandleTimeline(w http.ResponseWriter, r *http.Request) {
	limit := httputil.ParseIntParam(r, "limit", 50)
	entries, err := s.Mem.Timeline(r.Context(), limit)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Metrics.IncQuery()
	// Wire-shape mirror of memdomain.TimelineEntry. JSON tags live here
	// (transport concern) and not in the domain struct — same shape
	// returned by the pre-PHASE-2.1 timeline handler.
	out := make([]timelineJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, timelineJSON{
			ID:             e.ID,
			Category:       e.Category,
			Content:        e.Content,
			CreatedAt:      e.CreatedAt,
			Source:         e.Source,
			SourceType:     e.SourceType,
			ConversationID: e.ConversationID,
			MessageID:      e.MessageID,
		})
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// timelineJSON is the wire-shape mirror of memdomain.TimelineEntry.
// Lives in the transport shell so the domain struct stays JSON-less
// (single source of truth for wire encoding lives at the edge).
//
// Crucially: NO `omitempty` tags. Pre-PHASE-2.1 TimelineEntry in
// src/internal/server/memory/memory_service.go had no omitempty
// either — nil CreatedAt renders as `"created_at":null` and missing
// provenance fields render as `"source":""`. Dropping omitempty keeps
// the wire bytes identical so existing /timeline consumers don't see
// keys disappear.
type timelineJSON struct {
	ID             string     `json:"id"`
	Category       string     `json:"category"`
	Content        string     `json:"content"`
	CreatedAt      *time.Time `json:"created_at"`
	Source         string     `json:"source"`
	SourceType     string     `json:"source_type"`
	ConversationID string     `json:"conversation_id"`
	MessageID      string     `json:"message_id"`
}
