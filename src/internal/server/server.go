// Package server provides the HTTP API server with all handlers split into handlers.go,
// JSON utilities, middleware, and the standalone server entrypoint.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pavelveter/hermem/src/internal/algo"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/ingestion"
	"github.com/pavelveter/hermem/src/internal/metrics"
)

// ServerState holds schema-derived fields swapped atomically on SIGHUP.
type ServerState struct {
	Schema             core.SchemaConfig
	ValidCategories    map[string]bool
	ValidRelationTypes map[string]bool
}

// Server is the HTTP API server.
type Server struct {
	DB            *sql.DB
	VI            core.VectorIndex
	Worker        *ingestion.IngestionWorker
	Embedder      core.Embedder
	RetrievalOpts core.RetrieveContextOptions
	State         atomic.Pointer[ServerState]
}

// NewServer creates a Server.
func NewServer(db *sql.DB, vi core.VectorIndex, embedder core.Embedder, extractor core.LLMExtractor, dedupThreshold float32, retrievalOpts core.RetrieveContextOptions, schema core.SchemaConfig) *Server {
	validCategories := schema.AllowedCategories
	if validCategories == nil {
		validCategories = map[string]bool{}
	}
	validRelationTypes := schema.AllowedRelations
	if validRelationTypes == nil {
		validRelationTypes = map[string]bool{}
	}
	s := &Server{
		DB: db, VI: vi, Embedder: embedder,
		Worker:        ingestion.NewIngestionWorker(db, vi, extractor, embedder, dedupThreshold, schema),
		RetrievalOpts: retrievalOpts,
	}
	s.State.Store(&ServerState{Schema: schema, ValidCategories: validCategories, ValidRelationTypes: validRelationTypes})
	return s
}

// ReloadState atomically swaps schema state on SIGHUP.
func (s *Server) ReloadState(schema core.SchemaConfig, ranking core.RankingWeight, reranker core.Reranker) {
	cats := schema.AllowedCategories
	if cats == nil {
		cats = map[string]bool{}
	}
	rels := schema.AllowedRelations
	if rels == nil {
		rels = map[string]bool{}
	}
	s.State.Store(&ServerState{Schema: schema, ValidCategories: cats, ValidRelationTypes: rels})
	s.Worker.ReloadSchema(schema)
	s.RetrievalOpts.RankingWeight = ranking
	s.RetrievalOpts.Reranker = reranker
}

// --- JSON utilities ---

// WriteJSON encodes data as JSON and writes it with the given status code.
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, core.ErrorResponse{Error: msg})
}

// WriteErrorWithCode writes a structured error with code and field.
func WriteErrorWithCode(w http.ResponseWriter, status int, msg, code, field string) {
	WriteJSON(w, status, core.ErrorResponse{Error: msg, Code: code, Field: field})
}

// DecodeStrict parses JSON while rejecting unknown fields and trailing data.
func DecodeStrict(r io.Reader, dst interface{}) (code, field, msg string, ok bool) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
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
		return "invalid_type", typeErr.Field, fmt.Sprintf("invalid type for field %q", typeErr.Field), false
	}
	if strings.HasPrefix(err.Error(), "json: unknown field") {
		fn := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return "unknown_field", fn, "unknown field: " + fn, false
	}
	return "bad_json", "", "invalid json: " + err.Error(), false
}

// parseIntParam reads an int query parameter with a default.
func parseIntParam(r *http.Request, name string, def int) int {
	if s := r.URL.Query().Get(name); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}

// --- Standalone server (used by main.go "serve" CLI) ---

// StartStandaloneConfig bundles the wiring for the standalone HTTP server.
type StartStandaloneConfig struct {
	DB                *sql.DB
	VI                core.VectorIndex
	Embedder          core.Embedder
	Extractor         core.LLMExtractor
	Reranker          core.Reranker
	Schema            core.SchemaConfig
	Ranking           core.RankingWeight
	DedupThreshold    float32
	DepthCeiling      int
	MaxRetrievedNodes int
	Retention         core.RetentionPolicy
	APIKey            string
	Port              string
}

// StartStandalone starts an HTTP server with GC + graceful shutdown. Blocks until SIGINT/SIGTERM.
func StartStandalone(cfg StartStandaloneConfig) error {
	if cfg.Port == "" {
		cfg.Port = "8420"
	}

	srv := NewServer(cfg.DB, cfg.VI, cfg.Embedder, cfg.Extractor, cfg.DedupThreshold,
		core.RetrieveContextOptions{
			DepthCeiling:      cfg.DepthCeiling,
			MaxRetrievedNodes: cfg.MaxRetrievedNodes,
			RankingWeight:     cfg.Ranking,
			Reranker:          cfg.Reranker,
		},
		cfg.Schema)

	gcCtx, gcCancel := context.WithCancel(context.Background())
	gcDone := make(chan struct{})
	go func() { algo.GarbageCollector(gcCtx, cfg.DB, cfg.VI, cfg.Retention); close(gcDone) }()

	var handler http.Handler = registerRoutes(srv)
	handler = SlogMiddleware(handler)
	handler = RequestIDMiddleware(APIKeyMiddleware(cfg.APIKey)(handler))
	handler = RecoveryMiddleware(handler)

	httpSrv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("server ready", "port", cfg.Port)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-quit
	slog.Info("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown", "err", err)
	}
	shutdownCancel()
	gcCancel()
	<-gcDone
	slog.Info("server stopped")
	return nil
}

// registerRoutes wires every URL pattern on the standard mux.
func registerRoutes(srv *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.HandleHealth)
	mux.HandleFunc("/health/live", srv.HandleHealthLive)
	mux.HandleFunc("/health/ready", srv.HandleHealthReady)
	mux.HandleFunc("/metrics", metrics.MetricsHandler)
	mux.HandleFunc("/store", srv.HandleStore)
	mux.HandleFunc("/search", srv.HandleSearch)
	mux.HandleFunc("/retrieve", srv.HandleRetrieve)
	mux.HandleFunc("/ingest", srv.HandleIngest)
	mux.HandleFunc("/query", srv.HandleQuery)
	mux.HandleFunc("/edge", srv.HandleEdge)
	mux.HandleFunc("/task/status", srv.HandleTaskStatus)
	mux.HandleFunc("/task/executable", srv.HandleTaskExecutable)
	mux.HandleFunc("/task/next", srv.HandleTaskExecutable)
	mux.HandleFunc("/task/list", srv.HandleTaskList)
	mux.HandleFunc("/task/show", srv.HandleTaskShow)
	mux.HandleFunc("/task/dep", srv.HandleTaskDep)
	mux.HandleFunc("/task/tree", srv.HandleTaskTree)
	mux.HandleFunc("/task/create", srv.HandleTaskCreate)
	mux.HandleFunc("/task/rollback", srv.HandleTaskRollback)
	mux.HandleFunc("/query/explain", srv.HandleQueryExplain)
	mux.HandleFunc("/contradictions", srv.HandleContradictions)
	mux.HandleFunc("/timeline", srv.HandleTimeline)
	mux.HandleFunc("/provenance", srv.HandleProvenance)
	mux.HandleFunc("/recovery-plan", srv.HandleRecoveryPlan)
	mux.HandleFunc("/connected-components", srv.HandleConnectedComponents)
	mux.HandleFunc("/communities", srv.HandleCommunities)
	mux.HandleFunc("/admin/re-embed", srv.HandleReEmbed)
	mux.HandleFunc("/response", srv.HandleResponse)
	return mux
}
