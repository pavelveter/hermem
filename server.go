package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Server struct {
	db            *sql.DB
	worker        *IngestionWorker
	embedder      Embedder
	retrievalOpts RetrieveContextOptions
}

type StoreRequest struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
}

type SearchRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

type RetrieveRequest struct {
	SeedIDs  []string `json:"seed_ids"`
	MaxDepth int      `json:"max_depth"`
}

type IngestRequest struct {
	Dialog string `json:"dialog"`
}

// ErrorResponse carries a human message plus an optional (code, field)
// pair so clients can route the rejection without parsing prose. Both
// optional fields are omitempty so non-strict errors (method-not-allowed,
// missing-required-field) stay shape-compatible with the pre-PR7 wire
// contract.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
	Field string `json:"field,omitempty"`
}

func NewServer(db *sql.DB, embedder Embedder, extractor LLMExtractor, dedupThreshold float32, retrievalOpts RetrieveContextOptions) *Server {
	return &Server{
		db:            db,
		worker:        NewIngestionWorker(db, extractor, embedder, dedupThreshold),
		embedder:      embedder,
		retrievalOpts: retrievalOpts,
	}
}

// decodeStrict parses JSON from an io.Reader into dst while rejecting
// unknown fields via encoding/json.DisallowUnknownFields. On any
// failure it returns a (code, field, msg, ok) tuple that the caller
// forwards to writeErrorWithCode so clients get a structured rejection
// distinguishing empty_body / unknown_field / invalid_type / bad_json.
// The bool is false on any decode error.
//
// Used uniformly by the HTTP handlers and the CLI JSON-stdin parsers
// (main.go wraps stdin in bytes.NewReader) so the wire contract is
// identical end-to-end: an unknown field, an empty body, or a
// wrong-typed field yields the same response shape in either surface.
func decodeStrict(r io.Reader, dst interface{}) (code, field, msg string, ok bool) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
		// Strict-mode invariant: exactly one JSON value per request.
		// dec.More() returns true if more bytes remain in the buffer
		// after a successful Decode, which catches `{...}{...}` and
		// `{...} garbage` — both of which would otherwise silently
		// consume only the first object.
		if dec.More() {
			return "trailing_data", "", "trailing data after JSON value", false
		}
		return "", "", "", true
	}
	if errors.Is(err, io.EOF) {
		return "empty_body", "", "request body is empty", false
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return "invalid_type", typeErr.Field,
			fmt.Sprintf("invalid type for field %q (got %s, want %s)",
				typeErr.Field, typeErr.Value, typeErr.Type), false
	}
	if strings.HasPrefix(err.Error(), "json: unknown field") {
		// err.Error() looks like: json: unknown field "foo"
		rest := strings.TrimPrefix(err.Error(), "json: unknown field ")
		fieldName := strings.Trim(rest, "\"")
		return "unknown_field", fieldName, "unknown field: " + fieldName, false
	}
	return "bad_json", "", "invalid json: " + err.Error(), false
}

func (s *Server) HandleStore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req StoreRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.ID == "" || req.Category == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, "id, category, content required")
		return
	}

	entity := Entity{
		ID:        req.ID,
		Category:  req.Category,
		Content:   req.Content,
		Embedding: req.Embedding,
	}

	if err := StoreEntityWithEmbedding(s.db, entity); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req SearchRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query required")
		return
	}

	if req.TopK <= 0 {
		req.TopK = 5
	}

	embedding, err := s.embedder.Embed(req.Query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("embed failed: %v", err))
		return
	}

	results, err := SearchByVector(s.db, embedding, req.TopK)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, results)
}

func (s *Server) HandleRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req RetrieveRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if len(req.SeedIDs) == 0 {
		writeError(w, http.StatusBadRequest, "seed_ids required")
		return
	}

	if req.MaxDepth <= 0 {
		req.MaxDepth = 2
	}

	opts := s.retrievalOpts
	opts.MaxDepth = req.MaxDepth
	result, err := RetrieveContext(s.db, req.SeedIDs, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) HandleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req IngestRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.Dialog == "" {
		writeError(w, http.StatusBadRequest, "dialog required")
		return
	}

	if err := s.worker.ProcessDialog(req.Dialog); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) HandleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req SearchRequest
	if code, field, msg, ok := decodeStrict(r.Body, &req); !ok {
		writeErrorWithCode(w, http.StatusBadRequest, msg, code, field)
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query required")
		return
	}

	if req.TopK <= 0 {
		req.TopK = 3
	}

	context, err := GenerateResponse(s.db, s.embedder, s.retrievalOpts, req.Query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"context": context})
}

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError carries a single ErrorResponse. Both Code and Field default
// to "" and are omitted via omitempty, so this remains wire-compatible
// with the pre-PR7 response shape for callers that never trigger a
// strict-decode rejection.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

// writeErrorWithCode pairs the human message with the structured code
// + field pair, used by strictDecode rejections so clients can
// programmatically distinguish empty_body, unknown_field, invalid_type,
// and bad_json without parsing prose.
func writeErrorWithCode(w http.ResponseWriter, status int, msg, code, field string) {
	writeJSON(w, status, ErrorResponse{
		Error: msg,
		Code:  code,
		Field: field,
	})
}
