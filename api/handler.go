package api

import (
	"encoding/json"
	"net/http"
)

// Handler serves the OpenAPI spec as JSON and YAML.
//
// The handler is intentionally stateless: it pulls the current spec
// from GenerateSpec() on every request. The underlying spec is cached
// behind an atomic.Pointer (see api/spec.go) so reads are cheap, and an
// SIGHUP-driven api.InvalidateSpec() flushes the cache so the next
// request observes a freshly-built spec without a process restart.
type Handler struct{}

// NewHandler returns a new API spec handler.
func NewHandler() *Handler {
	return &Handler{}
}

// Routes returns the routes for the OpenAPI spec endpoints.
func (h *Handler) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"GET /openapi.json": h.handleJSON,
		"GET /openapi.yaml": h.handleYAML,
	}
}

func (h *Handler) handleJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Cache-Control: no-cache forces every client fetch to revalidate
	// with origin. This matters now that the OpenAPI spec is
	// invalidate-able via SIGHUP (api.InvalidateSpec in cli/serve.go);
	// without no-cache, intermediaries / browsers would serve a stale
	// spec for up to an hour after a SIGHUP. The server-side cost is
	// trivial: specCache.Load() is a single atomic-pointer read.
	w.Header().Set("Cache-Control", "no-cache")
	spec := GenerateSpec()
	b, err := spec.JSON()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to marshal JSON"})
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func (h *Handler) handleYAML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	// See handleJSON for the rationale behind no-cache vs max-age.
	w.Header().Set("Cache-Control", "no-cache")
	spec := GenerateSpec()
	b, err := spec.MarshalYAML()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to marshal YAML"})
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
