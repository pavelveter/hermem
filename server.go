package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
)

type Server struct {
	db      *sql.DB
	worker  *IngestionWorker
	embedder Embedder
}

type StoreRequest struct {
	ID       string    `json:"id"`
	Category string    `json:"category"`
	Content  string    `json:"content"`
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

type ErrorResponse struct {
	Error string `json:"error"`
}

func NewServer(db *sql.DB, embedder Embedder, extractor LLMExtractor, dedupThreshold float32) *Server {
	return &Server{
		db:       db,
		worker:   NewIngestionWorker(db, extractor, embedder, dedupThreshold),
		embedder: embedder,
	}
}

func (s *Server) HandleStore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req StoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.ID == "" || req.Category == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, "id, category, content required")
		return
	}

	entity := Entity{
		ID:       req.ID,
		Category: req.Category,
		Content:  req.Content,
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if len(req.SeedIDs) == 0 {
		writeError(w, http.StatusBadRequest, "seed_ids required")
		return
	}

	if req.MaxDepth <= 0 {
		req.MaxDepth = 2
	}

	result, err := RetrieveContext(s.db, req.SeedIDs, req.MaxDepth)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query required")
		return
	}

	if req.TopK <= 0 {
		req.TopK = 3
	}

	context, err := GenerateResponse(s.db, s.embedder, req.Query)
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

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}
