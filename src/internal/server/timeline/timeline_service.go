// Package timeline_http exposes timeline.Service over HTTP.
//
// PHASE 3.5 — moves the /timeline route out of
// src/internal/server/memory/ into this new shell following the
// PHASE 3.1 + 3.2 + 3.3 + 3.4 transport-extraction pattern. The
// memory HTTP shell no longer owns /timeline — the timeline HTTP
// shell owns it exclusively. The /timeline URL stays byte-identical
// so existing clients see no URL drift between PHASE 3.4 and
// PHASE 3.5.
//
// Timeline is a read-only surface (no schema gates, no SIGHUP-raced
// mutation) so the HTTP shell holds ONLY {Svc, Metrics} — no Refs.
// This is the smallest HTTPService shape across the chain; the
// absence of Refs is deliberate, not an oversight, since the daemon
// has nothing to swap on timeline rows.
package timeline_http

import (
	"net/http"
	"time"

	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/timeline"
)

// HTTPService is the transport shell for timeline.Service. Holds the
// borrowed *timeline.Service pointer + observability. No Refs because
// timeline is read-only and has no SIGHUP-raced mutation.
type HTTPService struct {
	Svc     *timeline.Service
	Metrics *metrics.Metrics
}

// New constructs a timeline HTTPService. Svc is required; the handler
// returns 500 if nil is somehow dispatched (defensive against
// future-zero-value wiring).
func New(svc *timeline.Service, m *metrics.Metrics) *HTTPService {
	return &HTTPService{Svc: svc, Metrics: m}
}

// Routes returns the URL → handler mapping. Wired by Server in
// src/internal/server/server.go via the per-service Routes() protocol.
// /timeline GET moved here from the memory shell in PHASE 3.5.
func (h *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/timeline": h.HandleTimeline,
	}
}

// HandleTimeline — GET /timeline[?limit=N]. Returns the N
// most-recently created entities (raw SQL — not agent-derived).
// Behaves identically to the pre-PHASE-3.5 server/memory
// HandleTimeline; only the underlying domain Service pointer changed
// (memory → timeline).
func (h *HTTPService) HandleTimeline(w http.ResponseWriter, r *http.Request) {
	limit := httputil.ParseIntParam(r, "limit", 50)
	entries, err := h.Svc.Timeline(r.Context(), limit)
	if err != nil {
		h.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.Metrics.IncQuery()
	// Wire-shape mirror of timeline.TimelineEntry. JSON tags live here
	// (transport concern) and not in the domain struct — same shape
	// returned by the pre-PHASE-3.5 timeline handler so existing /timeline
	// consumers do not see a contract drift.
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

// timelineJSON is the wire-shape mirror of timeline.TimelineEntry.
// Lives in the transport shell so the domain struct stays JSON-less
// (single source of truth for wire encoding lives at the edge).
//
// Crucially: NO `omitempty` tags. Pre-PHASE-3.5 TimelineEntry in
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
