package health

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/health"
	"github.com/pavelveter/hermem/src/internal/httputil"
)

type HTTPService struct {
	Svc *health.Service
}

func New(svc *health.Service) *HTTPService {
	return &HTTPService{Svc: svc}
}

func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/health":         s.HandleHealth,
		"/health/live":    s.HandleHealthLive,
		"/health/ready":   s.HandleHealthReady,
		"/health/startup": s.HandleHealthStartup,
	}
}

func (s *HTTPService) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, s.Svc.Health())
}

func (s *HTTPService) HandleHealthLive(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, s.Svc.Live())
}

func (s *HTTPService) HandleHealthReady(w http.ResponseWriter, r *http.Request) {
	st := s.Svc.Ready(r.Context())
	code := http.StatusOK
	if !st.Ready {
		code = http.StatusServiceUnavailable
	}
	httputil.WriteJSON(w, code, st)
}

func (s *HTTPService) HandleHealthStartup(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
