// Package ingest exposes ingest.Service over HTTP. Per-call worker
// construction (Service holds no long-lived IngestionWorker) —
// SIGHUP-race-free by design.
//
// §3.2 — embeds shared.BaseHTTPService; DedupThreshold stays as a
// shell-local field (per-shell snapshot semantics).
package ingest

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/ingest"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/server/shared"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService is the transport shell for ingest.Service. Holds the
// borrowed ingest.Service pointer + observability + the serverstate.Ref
// for schema-per-call reads + the dedupThreshold forwarded to
// ingest.Service.Ingest for the LLM extraction pipeline. embedder /
// extractor live inside the domain Service — no transport-level
// duplication.
type HTTPService struct {
	Svc            *ingest.Service
	DedupThreshold float32
	shared.BaseHTTPService
}

// New constructs an HTTPService. DedupThreshold is captured at boot
// from cfg.DedupThreshold; SIGHUP does NOT propagate dedup changes.
func New(svc *ingest.Service, m *metrics.Metrics, refs *serverstate.Ref, dedupThreshold float32) *HTTPService {
	return &HTTPService{
		Svc:            svc,
		DedupThreshold: dedupThreshold,
		BaseHTTPService: shared.BaseHTTPService{
			Metrics: m,
			Refs:    refs,
		},
	}
}

// Routes returns the URL → handler mapping. Wired by Server in
// src/internal/server/server.go via the per-service Routes() protocol.
//
//	/ingest        POST
//	/ingest/jobs   GET  — synchronous ingest has no async job tracker;
//	                       returns empty list + canonical "no jobs
//	                       tracked" envelope.
func (h *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/ingest":      h.Wrap(h.HandleIngest),
		"/ingest/jobs": h.Wrap(h.HandleJobs),
	}
}

// HandleIngest — POST /ingest. Drains a dialog through the LLM
// extractor and ingests all extracted entities + relations.
//
// §3.2 — error-returning handler. Transport-level rejections
// (405, missing dialog) WriteError + return nil; domain errors
// flow as err so h.Wrap fires the IncErr + mapStatus write.
func (h *HTTPService) HandleIngest(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	req, err := httputil.DecodeJSON[core.IngestRequest](w, r)
	if err != nil {
		return err
	}
	if req.Dialog == "" {
		httputil.WriteErrorWithCode(w, http.StatusUnprocessableEntity, &core.DomainError{Code: core.CodeInvalidInput, Message: "dialog required", Field: "dialog"})
		return nil
	}
	state := h.Refs.Load()
	if err := h.Svc.Ingest(r.Context(), req.Dialog, h.DedupThreshold, state.Schema); err != nil {
		return err
	}
	h.Metrics.IncIngest()
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}

// jobsResponse is the /ingest/jobs envelope. Jobs is always an empty
// slice (nil → []) because synchronous ingest has no job tracker; the
// Message field carries the contract for clients so a 200 response
// is unambiguously "no tracked jobs" rather than "internal error".
type jobsResponse struct {
	Jobs    []Job  `json:"jobs"`
	Message string `json:"message"`
}

// Job is the per-entry shape (the current empty-list return
// satisfies the type without surfacing any optional fields).
type Job struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// HandleJobs — GET /ingest/jobs. Returns the canonical empty-list
// envelope.
//
// §3.2 — error-returning shape, but no domain-error path; only the
// 405 method check may return err. Always-nil routes the response
// through h.Wrap's no-op success branch.
func (h *HTTPService) HandleJobs(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	httputil.WriteJSON(w, http.StatusOK, jobsResponse{
		Jobs:    []Job{},
		Message: "no jobs tracked (sync ingest only)",
	})
	return nil
}
