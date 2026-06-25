// Package memory hosts the write-side HTTP transport for the memory
// subsystem, POST-PHASE 3.5.
//
// After PHASE 3.4 + PHASE 3.5 the HTTP shell exposes ONLY /store.
// The /edge route moved to src/internal/server/edge/ (PHASE 3.5); the
// /timeline route moved to src/internal/server/timeline/ (PHASE 3.5).
// The /ingest route moved to src/internal/server/ingest/ (PHASE 3.4).
// URLs are byte-identical so existing clients see no drift between
// PHASE 3.3 and PHASE 3.5.
//
// Domain logic still lives in src/internal/memory (Service); this
// package owns the only remaining transport concerns: POST /store
// JSON encoding, method checks, request-body limits, schema-conflict
// cross-state guard, and metrics increments.
package memory

import (
	"errors"
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	memdomain "github.com/pavelveter/hermem/src/internal/memory"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService is the transport shell for the write-side domain. Holds
// the domain Service (Mem), the metrics counters (Metrics), the
// serverstate.Ref for schema-conflict checks (Refs), and the
// dedupThreshold kept construction-time for any future memory-write
// extractor hook (post-PHASE 3.4 the value is unused on this shell —
// ingest upstream uses its own copy).
type HTTPService struct {
	Mem            *memdomain.Service
	Metrics        *metrics.Metrics
	Refs           *serverstate.Ref
	DedupThreshold float32
}

// New constructs an HTTPService. DedupThreshold is captured at boot —
// unused after PHASE 3.4 (ingest owns its own copy); signature kept
// for caller-parity with the pre-PHASE-3.4 shell.
func New(mem *memdomain.Service, m *metrics.Metrics, refs *serverstate.Ref, dedupThreshold float32) *HTTPService {
	return &HTTPService{Mem: mem, Metrics: m, Refs: refs, DedupThreshold: dedupThreshold}
}

// Routes returns the URL → handler mapping. POST-PHASE 3.5 only /store
// remains — /edge moved to server/edge/, /timeline moved to
// server/timeline/, /ingest moved to server/ingest/ (PHASE 3.4). Wired
// up by Server in src/internal/server/server.go via the per-service
// Routes() protocol.
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/store": s.HandleStore,
	}
}

// rejectSchemaConflict writes the canonical 409 envelope and returns
// true if the schema generation observed at handler entry differs from
// the live Refs (SIGHUP swapped mid-request). POST-PHASE 3.5 only
// HandleStore uses this; the previous HandleEdge + HandleTimeline call
// sites lifted with their respective shells.
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
// tests don't see a status drift. POST-PHASE 3.5 only HandleStore uses
// this; edge pkg has its own equivalent for relation_type validation.
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
// its historical non-linking behaviour). After PHASE 3.4 + 3.5 this is
// the one handler left on the memory shell.
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
