// Package server provides the HTTP API shell.
//
// After the god-object split this package is just a dispatcher + lifecycle
// manager. Domain logic lives in the per-sub-domain sub-packages:
//
//   - server/retrieval/     — search, retrieve, query, response, query_explain, provenance
//   - server/task/          — task/status, executable, list, show, dep, tree, create, rollback, recovery-plan
//   - server/memory/        — HTTP shell for /store, /ingest, /edge, /timeline. Delegates
//     domain logic to internal/memory.Service.
//   - server/contradiction/ — GET /contradictions[?id=X]. Delegates to internal/contradiction.Service.
//     Added in PHASE 2.3 (previously /contradictions lived in server/retrieval
//     behind a temporary retrieval.Service.DB() reach-through).
//   - server/graph/         — /connected-components, /communities, /graph/verify. Delegates
//     to internal/graph.Service. Added in PHASE 3.1 (previously /connected-components
//     and /communities lived on AdminService as a god-object).
//   - server/migration/     — /db/migrate, /db/rollback, /db/verify, /db/schema. Delegates
//     to internal/migration.Service. Added in PHASE 3.2 (the four db/* routes previously
//     lived as CLI-only in cli/db/{migrate,schema,verify,rollback}.go).
//   - server/retention/     — POST /admin/retention/run. Delegates to
//     internal/retention.Service. Added in PHASE 3.3 (the retention
//     goroutine previously lived in server/server.go Serve as a raw
//     algo.GarbageCollector call; no HTTP surface existed pre-PHASE-3.3).
//   - server/               — AdminService: health, metrics, admin/re-embed (graph
//     analytics routes were extracted in PHASE 3.1; retention is owned
//     by server/retention now, the server.go Serve() method only hosts
//     the lifecycle goroutine).
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

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/retention"
	cnd "github.com/pavelveter/hermem/src/internal/server/contradiction"
	graphsrv "github.com/pavelveter/hermem/src/internal/server/graph"
	mem "github.com/pavelveter/hermem/src/internal/server/memory"
	migrsrv "github.com/pavelveter/hermem/src/internal/server/migration"
	retsrv "github.com/pavelveter/hermem/src/internal/server/retention"
	ret "github.com/pavelveter/hermem/src/internal/server/retrieval"
	tasksvc "github.com/pavelveter/hermem/src/internal/server/task"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// Server is the HTTP shell. It holds the 8 services + a mux + the atomic
// state holder. No domain fields (no DB / VI / Embedder directly —
// memory's domain service lives in internal/memory, contradiction's in
// internal/contradiction, task's in internal/task, graph's in
// internal/graph, migration's in internal/migration, retention's in
// internal/retention; each transport shell holds the domain Service
// reference and threads it as a borrowed pointer).
type Server struct {
	Refs          *serverstate.Ref
	Retrieval     *ret.HTTPService
	Task          *tasksvc.HTTPService
	Memory        *mem.HTTPService
	Contradiction *cnd.HTTPService
	Graph         *graphsrv.HTTPService
	Migration     *migrsrv.HTTPService
	Retention     *retsrv.HTTPService
	Admin         *AdminService
	mux           *http.ServeMux
}

// NewServer wires the 8 services into a single mux. No HTTP server is started
// — call (*Server).ServeHTTP separately (e.g. via the convenience Run below).
//
// PHASE 2.3 added the contradiction *HTTPService argument; PHASE 2.4
// renamed the task *Service argument to *HTTPService to mirror the
// post-2.4 transport-shell shape (server/task.Service → server/task.HTTPService);
// PHASE 3.1 inserts the graph *HTTPService argument between contradiction
// and admin, lifting /connected-components + /communities out of the
// god-object AdminService. PHASE 3.2 inserts the migration *HTTPService
// argument between graph and admin, exposing 4 NEW routes that had no
// HTTP surface previously. PHASE 3.3 inserts the retention *HTTPService
// argument between migration and admin, replacing the raw
// algo.GarbageCollector goroutine inside Serve() with
// retention.Service.Run, plus exposing POST /admin/retention/run (the
// FIRST HTTP route for retention — no HTTP surface existed pre-PHASE-3.3).
func NewServer(refs *serverstate.Ref, retrieval *ret.HTTPService, task *tasksvc.HTTPService, memory *mem.HTTPService, contradiction *cnd.HTTPService, graph *graphsrv.HTTPService, migration *migrsrv.HTTPService, retention *retsrv.HTTPService, admin *AdminService) *Server {
	s := &Server{
		Refs:          refs,
		Retrieval:     retrieval,
		Task:          task,
		Memory:        memory,
		Contradiction: contradiction,
		Graph:         graph,
		Migration:     migration,
		Retention:     retention,
		Admin:         admin,
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
	for path, hf := range s.Contradiction.Routes() {
		mux.HandleFunc(path, hf)
	}
	for path, hf := range s.Graph.Routes() {
		mux.HandleFunc(path, hf)
	}
	for path, hf := range s.Migration.Routes() {
		mux.HandleFunc(path, hf)
	}
	for path, hf := range s.Retention.Routes() {
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
	// PHASE 3.3: replace the raw algo.GarbageCollector call with the
	// transport-agnostic retention.Service.Run loop. The closure
	// captures cfg.Retention at boot — by design, SIGHUP does NOT
	// propagate retention policy changes. The retention.Service is
	// constructed fresh inside the goroutine because nothing about
	// its state needs to persist beyond a single Run loop.
	go func() { svc := retention.NewService(cfg.DB, cfg.VI); svc.Run(gcCtx, cfg.Retention); close(gcDone) }()

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
