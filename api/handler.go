package api

import (
	"encoding/json"
	"net/http"
)

// Handler serves the OpenAPI spec as JSON and YAML.
type Handler struct {
	spec *Spec
}

// NewHandler returns a new API spec handler.
func NewHandler() *Handler {
	return &Handler{spec: GenerateSpec()}
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
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.spec.JSON())
}

func (h *Handler) handleYAML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	b, err := h.spec.MarshalYAML()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to marshal YAML"})
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
