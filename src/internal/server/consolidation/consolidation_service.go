package consolidation

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/consolidation"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/server/shared"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

type HTTPService struct {
	Svc           *consolidation.Service
	DefaultPolicy consolidation.Policy
	shared.BaseHTTPService
}

func New(svc *consolidation.Service, m *metrics.Metrics, refs *serverstate.Ref, defaultPolicy consolidation.Policy) *HTTPService {
	return &HTTPService{
		Svc:           svc,
		DefaultPolicy: defaultPolicy,
		BaseHTTPService: shared.BaseHTTPService{
			Metrics: m,
			Refs:    refs,
		},
	}
}

func (h *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/admin/consolidate": h.Wrap(h.HandleConsolidate),
	}
}

func (h *HTTPService) HandleConsolidate(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	h.Metrics.IncConsolidationRun()
	rep, err := h.Svc.RunOnce(r.Context(), h.DefaultPolicy)
	if err != nil {
		httputil.WriteJSON(w, http.StatusInternalServerError, rep)
		return nil
	}
	httputil.WriteJSON(w, http.StatusOK, rep)
	return nil
}
