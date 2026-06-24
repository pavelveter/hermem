// Package server provides the HTTP API shell.
//
// After the god-object split this package is just a dispatcher + lifecycle
// manager. Domain logic lives in the 4 sub-packages plus the extracted
// domain Service for memory:
//
//   - server/retrieval/  — search, retrieve, query, response, query_explain, provenance, contradictions
//   - server/task/       — task/status, executable, list, show, dep, tree, create, rollback, recovery-plan
//   - server/memory/     — HTTP shell for /store, /ingest, /edge, /timeline (POST and GET). Delegates
//     domain logic to internal/memory.Service.
//   - server/            — AdminService: health, metrics, connected-components, communities, re-embed
//
// Shared per-request configuration is read atomically via *serverstate.Ref —
// concurrent SIGHUP-driven state swaps are safe with in-flight handlers.
package server

import (
	"context"
	"database/sql"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pavelveter/hermem/src/internal/algo"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	mem "github.com/pavelveter/hermem/src/internal/server/memory"
	ret "github.com/pavelveter/hermem/src/internal/server/retrieval"
	tasksvc "github.com/pavelveter/hermem/src/internal/server/task"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// Server is the HTTP shell. It holds the 4 services + a mux + the atomic
// state holder. No domain fields (no DB / VI / Embedder directly —
// memory's domain service lives in internal/memory and is borrowed
// by the server/memory shell as a Service reference).
type Server struct {
	Refs      *serverstate.Ref
	Retrieval *ret.Service
	Task      *tasksvc.Service
	Memory    *mem.HTTPService
	Admin     *AdminService
	mux       *http.ServeMux
}

// NewServer wires the 4 services into a single mux. No HTTP server is started
// — call (*Server).ServeHTTP separately (e.g. via the convenience Run below).
func NewServer(refs *serverstate.Ref, retrieval *ret.Service, task *tasksvc.Service, memory *mem.HTTPService, admin *AdminService) *Server {
	s := &Server{
		Refs:      refs,
		Retrieval: retrieval,
		Task:      task,
		Memory:    memory,
		Admin:     admin,
	}
	s.mount()
	return s
}

// mount wires every URL on the standard mux. /task/executable and /task/next
// both route to HandleTaskExecutable — both keys are distinct so both register.
func (s *Server) mount() {
	mux := http.NewServeMux()
	for path, hf := range s.Retrieval.Routes() {
		mux.HandleFunc(path, hf)
	}
	for path, hf := range s.Task.Routes() {
		mux.HandleFunc(path, hf)
	}
	for path, hf := range s.Memory.Routes() {
		mux.HandleFunc(path, hf)
	}
	for path, hf := range s.Admin.Routes() {
		mux.HandleFunc(path, hf)
	}
	s.mux = mux
}

// ReloadState atomically swaps the configuration state. Safe to call
// concurrently with in-flight handlers — handlers always read
// s.Refs.Load() per request.
//
// Memory.HTTPService has no long-lived IngestionWorker (PHASE 2.1: worker
// is constructed per Ingest call), so no OnStateChange propagation is
// required. Schema mutations land on the next /ingest call that the
// caller makes, atomically capturing state.Schema from s.Refs.Load().
func (s *Server) ReloadState(newState *serverstate.State) {
	if newState == nil {
		panic("server: ReloadState called with nil state")
	}
	s.Refs.Store(newState)
}

// Mux exposes the wired mux for tests and tooling.
func (s *Server) Mux() *http.ServeMux { return s.mux }

// ServeConfig bundles the run-time dependencies the lifecycle needs in
// addition to the wired *Server (GC scope, auth key, listen port).
type ServeConfig struct {
	DB        *sql.DB
	VI        core.VectorIndex
	Retention core.RetentionPolicy
	APIKey    string
	Port      string
}

// Serve runs the HTTP listener + GC + graceful shutdown. Blocks until
// SIGINT/SIGTERM, then drains in order: HTTP → GC → return.
//
// Caller is expected to start its own SIGHUP goroutine that calls
// (*Server).ReloadState on config changes.
func (s *Server) Serve(cfg ServeConfig) error {
	if cfg.Port == "" {
		cfg.Port = "8420"
	}

	gcCtx, gcCancel := context.WithCancel(context.Background())
	gcDone := make(chan struct{})
	go func() { algo.GarbageCollector(gcCtx, cfg.DB, cfg.VI, cfg.Retention); close(gcDone) }()

	// Canonical middleware order (outer → inner; each line wraps the
	// previous, so the LAST wrap is the OUTERMOST):
	//   Recovery                — catches panics from any inner layer
	//   Timeout                 — derives r.Context() with a deadline
	//   Slog                    — logs after completion (sees original ctx for 499)
	//   RequestID               — echoes / generates X-Request-ID
	//   APIKey                  — 401 on bad X-API-Key
	//   MaxBytes                — wraps r.Body with MaxBytesReader
	//   SafeBodyClose           — drains + closes r.Body on every exit path
	//   mux handlers            — innermost; receives a fully-prepared request
	// Adding a middleware anywhere else breaks one of the contracts:
	// outside Recovery means a panic can tear down the listener; inside
	// SafeBodyClose means a handler's drain runs after the deferred
	// close and returns 0 bytes (silent loss under MaxBytes).
	var handler http.Handler = s.Mux()
	handler = SafeBodyCloseMiddleware(handler)
	handler = MaxBytesMiddleware(httputil.MaxBodyBytes)(handler)
	handler = SlogMiddleware(handler)
	handler = RequestIDMiddleware(APIKeyMiddleware(cfg.APIKey)(handler))
	handler = TimeoutMiddleware(120 * time.Second)(handler)
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
