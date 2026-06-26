// Package memory exposes memory.Service over HTTP. Embeds
// shared.BaseHTTPService for routing and metrics; /store is the only
// route. Owns transport-only concerns: JSON encoding, method check,
// request-body limits, schema-conflict guard, metrics.
//
// §3.2 — embeds shared.BaseHTTPService; DedupThreshold stays as a
// shell-local field (shell-local snapshot semantics). The pre-§3.2
// `status := 422 if shared.IsSchemaErr else 500` collapsible mapping
// disappears — server.mapStatus' CodeInvalidSchema branch handles
// the 422 routing automatically.
package memory

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	memdomain "github.com/pavelveter/hermem/src/internal/memory"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/server/shared"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService is the transport shell for the write-side domain. Holds
// the domain Service (Svc), the server-state Refs (via the embedded
// BaseHTTPService), the schema-conflict Metrics (also via base), and
// the dedupThreshold kept construction-time for any future memory-
// write extractor hook (post-PHASE 3.4 the value is unused on this
// shell — ingest upstream uses its own copy).
type HTTPService struct {
	Svc            *memdomain.Service
	DedupThreshold float32
	shared.BaseHTTPService
}

// New constructs an HTTPService. DedupThreshold is captured at boot —
// unused after PHASE 3.4 (ingest owns its own copy); signature kept
// for caller-parity with the pre-PHASE-3.4 shell.
func New(svc *memdomain.Service, m *metrics.Metrics, refs *serverstate.Ref, dedupThreshold float32) *HTTPService {
	return &HTTPService{
		Svc:            svc,
		DedupThreshold: dedupThreshold,
		BaseHTTPService: shared.BaseHTTPService{
			Metrics: m,
			Refs:    refs,
		},
	}
}

// Routes returns the URL → handler mapping. POST-PHASE 3.5 only /store
// remains — /edge moved to server/edge/, /timeline moved to
// server/timeline/, /ingest moved to server/ingest/ (PHASE 3.4).
//
// §3.2 — handler is wrapped via s.Wrap.
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/store": s.Wrap(s.HandleStore),
	}
}

// HandleStore — POST /store. Persists one entity with a caller-supplied
// embedding, then fires the HTTP-only AutoLinkEdges side effect via
// memdomain.Service.StoreAndLink (/store only — CLI /store preserves
// its historical non-linking behaviour). After PHASE 3.4 + 3.5 this is
// the one handler left on the memory shell.
//
// §3.2 — error-returning handler. The pre-§3.2 inline
// `status := 422 if shared.IsSchemaErr else 500` mapping disappears:
// that case is now handled by server.mapStatus' CodeInvalidSchema
// branch returning (422, msg). The handler simply returns err for
// s.Wrap to route; identical wire contract.
func (s *HTTPService) HandleStore(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	req, err := httputil.DecodeJSON[core.StoreRequest](w, r)
	if err != nil {
		return err
	}
	if req.ID == "" || req.Category == "" || req.Content == "" {
		httputil.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "id, category, content required", "invalid_input", "")
		return nil
	}
	state := s.Refs.Load()
	if !state.ValidCategories[req.Category] {
		httputil.WriteError(w, http.StatusUnprocessableEntity, "unknown category: "+req.Category)
		return nil
	}
	if shared.RejectSchemaConflict(w, state.Generation, s.Refs, s.Metrics) {
		return nil
	}
	if err := s.Svc.StoreAndLink(r.Context(), req, state.Schema); err != nil {
		return err
	}
	s.Metrics.IncStore()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}
