// Package server provides the HTTP API shell. Dispatcher + lifecycle
// manager: 12 typed HTTPService sub-shells (each implements
// RouteProvider) are mounted via Server.mount(); /metrics is registered
// directly from the Server's Metrics field.
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
	"github.com/pavelveter/hermem/src/internal/metrics"
	retentiondomain "github.com/pavelveter/hermem/src/internal/retention"
	cnd "github.com/pavelveter/hermem/src/internal/server/contradiction"
	"github.com/pavelveter/hermem/src/internal/server/edge"
	graphsrv "github.com/pavelveter/hermem/src/internal/server/graph"
	healthsrv "github.com/pavelveter/hermem/src/internal/server/health"
	ingsrv "github.com/pavelveter/hermem/src/internal/server/ingest"
	mem "github.com/pavelveter/hermem/src/internal/server/memory"
	migrsrv "github.com/pavelveter/hermem/src/internal/server/migration"
	"github.com/pavelveter/hermem/src/internal/server/reembed"
	"github.com/pavelveter/hermem/src/internal/server/retention"
	ret "github.com/pavelveter/hermem/src/internal/server/retrieval"
	tasksvc "github.com/pavelveter/hermem/src/internal/server/task"
	"github.com/pavelveter/hermem/src/internal/server/timeline"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// Server is the HTTP shell. It holds 12 domain HTTPService instances +
// a Metrics field for /metrics + a mux + the atomic state holder.
// Each transport shell holds the domain Service reference and threads
// it as a borrowed pointer.
//
// PHASE 3.8: AdminService dissolved — /metrics registered directly
// from the Metrics field in mount(). The AdminService god-object,
// dismantled across 5 phases, is now entirely gone.
type Server struct {
	Refs          *serverstate.Ref
	Retrieval     *ret.HTTPService
	Task          *tasksvc.HTTPService
	Memory        *mem.HTTPService
	Edge          *edge.HTTPService
	Timeline      *timeline.HTTPService
	Ingest        *ingsrv.HTTPService
	Contradiction *cnd.HTTPService
	Graph         *graphsrv.HTTPService
	Migration     *migrsrv.HTTPService
	Retention     *retention.HTTPService
	Reembed       *reembed.HTTPService
	Health        *healthsrv.HTTPService
	Metrics       *metrics.Metrics
	mux           *http.ServeMux
}

// NewServer wires the 12 domain services + Metrics into a single mux.
// No HTTP server is started — call (*Server).ServeHTTP separately
// (e.g. via the convenience Run below).
//
// PHASE 3.8: AdminService dissolved. The final `/metrics` route is
// registered directly from the Metrics field. 5 phases of extraction
// (3.1–3.5 initially from the god-object, then 3.6 reembed, 3.7 health,
// 3.8 metrics) eliminate AdminService entirely.
func NewServer(refs *serverstate.Ref, retrieval *ret.HTTPService, task *tasksvc.HTTPService, memory *mem.HTTPService, edgeSvc *edge.HTTPService, timelineSvc *timeline.HTTPService, ingest *ingsrv.HTTPService, contradiction *cnd.HTTPService, graph *graphsrv.HTTPService, migration *migrsrv.HTTPService, retentionSvc *retention.HTTPService, reembedSvc *reembed.HTTPService, health *healthsrv.HTTPService, m *metrics.Metrics) *Server {
	s := &Server{
		Refs:          refs,
		Retrieval:     retrieval,
		Task:          task,
		Memory:        memory,
		Edge:          edgeSvc,
		Timeline:      timelineSvc,
		Ingest:        ingest,
		Contradiction: contradiction,
		Graph:         graph,
		Migration:     migration,
		Retention:     retentionSvc,
		Reembed:       reembedSvc,
		Health:        health,
		Metrics:       m,
	}
	s.mount()
	return s
}

// mount wires every URL on the standard mux. Each typed shell is a
// RouteProvider; iterating the typed Server fields into a local
// []providerSlot lets a single registrations loop handle every shell
// while still naming each shell in the slog.Warn below. Adding a 13th
// shell requires: (a) add field to Server, (b) add arg to NewServer,
// (c) add entry below. Pre-§3.1 step (c) was a copy-pasted 3-line
// for-range; now it's one line.
//
// Nil providers warn + skip — never silently. A misconfigured
// deployment would otherwise boot with a half-wired mux and serve 404s
// on missing endpoints with no error. Production wiring that omits a
// shell logs an explicit warning so the operator catches the bug.
func (s *Server) mount() {
	mux := http.NewServeMux()
	slots := []providerSlot{
		{"Retrieval", s.Retrieval},
		{"Task", s.Task},
		{"Memory", s.Memory},
		{"Edge", s.Edge},
		{"Timeline", s.Timeline},
		{"Ingest", s.Ingest},
		{"Contradiction", s.Contradiction},
		{"Graph", s.Graph},
		{"Migration", s.Migration},
		{"Retention", s.Retention},
		{"Reembed", s.Reembed},
		{"Health", s.Health},
	}
	for _, slot := range slots {
		if slot.p == nil {
			slog.Warn("server: shell not wired, routes will be missing", "shell", slot.name)
			continue
		}
		for path, hf := range slot.p.Routes() {
			mux.HandleFunc(path, hf)
		}
	}
	// PHASE 3.8: /metrics registered directly — AdminService dissolved.
	mux.Handle("/metrics", s.Metrics.MetricsHandler()) // mux.Handle (NOT HandleFunc): MetricsHandler() returns http.Handler, while HandleFunc expects a func(http.ResponseWriter, *http.Request) value. Passing the method value without invocation would also type-mismatch.
	// Opt-in Go runtime profiling. Off by default — see RegisterPprof.
	RegisterPprof(mux)
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
	go func() { svc := retentiondomain.New(cfg.DB, cfg.VI); svc.Run(gcCtx, cfg.Retention); close(gcDone) }()

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
	handler = RequestIDMiddleware(AuthMiddleware()(handler))
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
