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
//   - server/edge/          — POST /edge. Delegates to internal/edge.Service.
//     Added in PHASE 3.5 (the /edge route previously lived on server/memory
//     shell; lifted out following the PHASE 3.1–3.4 transport-extraction
//     pattern. URL contract byte-identical).
//   - server/ingest/        — /ingest, /ingest/jobs. Delegates to internal/ingest.Service.
//     Added in PHASE 3.4 (the /ingest route previously lived on server/memory
//     shell; lifted out following the PHASE 3.1–3.3 transport-extraction
//     pattern. /ingest/jobs GET is NEW — synchronous ingest has no async
//     job tracker; the route returns a canonical empty-list envelope
//     until a future PHASE 3.x async-extraction lands).
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
//   - server/timeline/      — GET /timeline. Delegates to internal/timeline.Service.
//     Added in PHASE 3.5 (the /timeline route previously lived on server/memory
//     shell; lifted out following the PHASE 3.1–3.4 transport-extraction
//     pattern. URL contract byte-identical; wire-shape preserved verbatim
//     so existing /timeline consumers see no drift).
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
	edgesrv "github.com/pavelveter/hermem/src/internal/server/edge"
	graphsrv "github.com/pavelveter/hermem/src/internal/server/graph"
	ingsrv "github.com/pavelveter/hermem/src/internal/server/ingest"
	mem "github.com/pavelveter/hermem/src/internal/server/memory"
	migrsrv "github.com/pavelveter/hermem/src/internal/server/migration"
	retsrv "github.com/pavelveter/hermem/src/internal/server/retention"
	ret "github.com/pavelveter/hermem/src/internal/server/retrieval"
	tasksvc "github.com/pavelveter/hermem/src/internal/server/task"
	tlsrv "github.com/pavelveter/hermem/src/internal/server/timeline"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// Server is the HTTP shell. It holds the 11 services + a mux + the atomic
// state holder. No domain fields (no DB / VI / Embedder directly —
// memory's domain service lives in internal/memory, edge's in
// internal/edge (PHASE 3.5), timeline's in internal/timeline
// (PHASE 3.5), contradiction's in internal/contradiction, task's in
// internal/task, ingest's in internal/ingest (PHASE 3.4), graph's in
// internal/graph, migration's in internal/migration, retention's in
// internal/retention; each transport shell holds the domain Service
// reference and threads it as a borrowed pointer).
type Server struct {
	Refs          *serverstate.Ref
	Retrieval     *ret.HTTPService
	Task          *tasksvc.HTTPService
	Memory        *mem.HTTPService
	Edge          *edgesrv.HTTPService
	Timeline      *tlsrv.HTTPService
	Ingest        *ingsrv.HTTPService
	Contradiction *cnd.HTTPService
	Graph         *graphsrv.HTTPService
	Migration     *migrsrv.HTTPService
	Retention     *retsrv.HTTPService
	Admin         *AdminService
	mux           *http.ServeMux
}

// NewServer wires the 11 services into a single mux. No HTTP server is started
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
// PHASE 3.4 inserts the ingest *HTTPService argument between memory
// and contradiction, lifting /ingest out of the server/memory shell
// (and exposing the NEW GET /ingest/jobs surface). PHASE 3.5 inserts
// the edge + timeline *HTTPService arguments between memory and
// ingest, lifting /edge and /timeline out of the server/memory shell.
// The memory HTTP shell keeps only /store; URL contracts for /edge,
// /timeline, /ingest are byte-identical so existing clients see no
// drift.
func NewServer(refs *serverstate.Ref, retrieval *ret.HTTPService, task *tasksvc.HTTPService, memory *mem.HTTPService, edge *edgesrv.HTTPService, timeline *tlsrv.HTTPService, ingest *ingsrv.HTTPService, contradiction *cnd.HTTPService, graph *graphsrv.HTTPService, migration *migrsrv.HTTPService, retention *retsrv.HTTPService, admin *AdminService) *Server {
	s := &Server{
		Refs:          refs,
		Retrieval:     retrieval,
		Task:          task,
		Memory:        memory,
		Edge:          edge,
		Timeline:      timeline,
		Ingest:        ingest,
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
	for path, hf := range s.Edge.Routes() {
		mux.HandleFunc(path, hf)
	}
	for path, hf := range s.Timeline.Routes() {
		mux.HandleFunc(path, hf)
	}
	for path, hf := range s.Ingest.Routes() {
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
