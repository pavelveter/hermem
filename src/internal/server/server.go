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
	"log/slog"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/lifecycle"
	"github.com/pavelveter/hermem/src/internal/lifecycle/components"
	"github.com/pavelveter/hermem/src/internal/metrics"
	retentiondomain "github.com/pavelveter/hermem/src/internal/retention"
	cnd "github.com/pavelveter/hermem/src/internal/server/contradiction"
	"github.com/pavelveter/hermem/src/internal/server/edge"
	graphsrv "github.com/pavelveter/hermem/src/internal/server/graph"
	healthsrv "github.com/pavelveter/hermem/src/internal/server/health"
	ingsrv "github.com/pavelveter/hermem/src/internal/server/ingest"
	mem "github.com/pavelveter/hermem/src/internal/server/memory"
	migrsrv "github.com/pavelveter/hermem/src/internal/server/migration"
	"github.com/pavelveter/hermem/src/internal/server/ratelimit"
	"github.com/pavelveter/hermem/src/internal/server/reembed"
	"github.com/pavelveter/hermem/src/internal/server/retention"
	ret "github.com/pavelveter/hermem/src/internal/server/retrieval"
	tasksvc "github.com/pavelveter/hermem/src/internal/server/task"
	"github.com/pavelveter/hermem/src/internal/server/timeline"
	"github.com/pavelveter/hermem/src/internal/serverstate"

	apipkg "github.com/pavelveter/hermem/api"
)

// Server is the HTTP shell. It holds a registry of RouteProviders +
// a Metrics field for /metrics + a mux + the atomic state holder.
type Server struct {
	Refs      *serverstate.Ref
	Metrics   *metrics.Metrics
	providers []providerSlot
	mux       *http.ServeMux
	limiter   *ratelimit.LimiterRef
	envMgr    *clienv.EnvManager
	stopOnce  sync.Once
	stopFunc  func()
}

// ServerDeps holds all dependencies for creating a Server.
type ServerDeps struct {
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
}

// NewServerFromDeps wires the 12 domain services + Metrics into a single mux.
func NewServerFromDeps(deps ServerDeps) *Server {
	s := &Server{
		Refs:    deps.Refs,
		Metrics: deps.Metrics,
		providers: []providerSlot{
			{"Retrieval", deps.Retrieval},
			{"Task", deps.Task},
			{"Memory", deps.Memory},
			{"Edge", deps.Edge},
			{"Timeline", deps.Timeline},
			{"Ingest", deps.Ingest},
			{"Contradiction", deps.Contradiction},
			{"Graph", deps.Graph},
			{"Migration", deps.Migration},
			{"Retention", deps.Retention},
			{"Reembed", deps.Reembed},
			{"Health", deps.Health},
		},
	}
	s.mount()
	return s
}

// mount wires every URL on the standard mux. Each typed shell is a
// RouteProvider; iterating the typed Server fields into a local
// []providerSlot lets a single registrations loop handle every shell
// while still naming each shell in the slog.Warn below. Adding a 13th
// shell requires: (a) add field to Server, (b) add field to
// ServerDeps (the NewServerFromDeps parameter struct), (c) add entry
// below. Pre-§3.1 step (c) was a copy-pasted 3-line
// for-range; now it's one line.
//
// Nil providers warn + skip — never silently. A misconfigured
// deployment would otherwise boot with a half-wired mux and serve 404s
// on missing endpoints with no error. Production wiring that omits a
// shell logs an explicit warning so the operator catches the bug.
func (s *Server) mount() {
	mux := http.NewServeMux()
	for _, slot := range s.providers {
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
	// OpenAPI 3.1 spec endpoints.
	apiHandler := apipkg.NewHandler()
	for path, hf := range apiHandler.Routes() {
		mux.HandleFunc(path, hf)
	}
	s.mux = mux
}

// ReloadState atomically swaps the configuration state. Safe to call
// concurrently with in-flight handlers — handlers always read
// s.Refs.Load() per request.
//
// If rate limiting is enabled, a fresh Limiter is constructed and
// swapped in atomically. The old limiter's eviction goroutine is stopped.
// In-flight requests continue using the old limiter until they complete;
// new requests use the new one.
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

	// Swap rate limiter if enabled in new config.
	if s.limiter != nil && s.envMgr != nil {
		env := s.envMgr.Get()
		if env != nil && env.Cfg != nil && env.Cfg.RateLimitEnabled {
			rl := ratelimit.New(ratelimit.Config{
				RPS:           float64(env.Cfg.RateLimitRPS),
				Burst:         env.Cfg.RateLimitBurst,
				EvictInterval: 5 * time.Minute,
			})
			stop := rl.Start(5 * time.Minute)
			old := s.limiter.Swap(rl)
			// Stop old eviction goroutine.
			if old != nil {
				_ = old
			}
			s.stopOnce.Do(func() {
				if s.stopFunc != nil {
					s.stopFunc()
				}
			})
			s.stopFunc = stop
			slog.Info("rate limit reloaded",
				"rps", env.Cfg.RateLimitRPS,
				"burst", env.Cfg.RateLimitBurst,
			)
		}
	}
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
	Env       *clienv.Env
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
	//
	// RateLimit is wrapped OUTSIDE Slog so a 429 short-circuits
	// before Slog logs — under a volumetric DoS the 429s would
	// otherwise flood slog at the same rate as the attack. The
	// trade-off is that the operator loses 429 visibility in the
	// log stream; counters can be added later via the Metrics
	// package if operational visibility becomes a concern.
	var handler http.Handler = s.Mux()
	handler = SafeBodyCloseMiddleware(handler)
	handler = MaxBytesMiddleware(httputil.MaxBodyBytes)(handler)
	handler = SlogMiddleware(handler)
	if cfg.Env != nil && cfg.Env.Cfg != nil && cfg.Env.Cfg.RateLimitEnabled {
		rl := ratelimit.New(ratelimit.Config{
			RPS:           float64(cfg.Env.Cfg.RateLimitRPS),
			Burst:         cfg.Env.Cfg.RateLimitBurst,
			EvictInterval: 5 * time.Minute,
		})
		stop := rl.Start(5 * time.Minute)
		s.limiter = ratelimit.NewLimiterRef(rl)
		s.stopFunc = stop
		handler = ratelimit.Middleware(ratelimit.Options{
			LimiterRef:   s.limiter,
			KeyFunc:      ratelimit.ResolveKeyFunc(cfg.Env.Cfg.RateLimitKeyBy),
			ShouldBypass: ratelimit.BypassHealthAndMetrics(),
		})(handler)
		slog.Info("rate limit enabled",
			"rps", cfg.Env.Cfg.RateLimitRPS,
			"burst", cfg.Env.Cfg.RateLimitBurst,
			"key_by", cfg.Env.Cfg.RateLimitKeyBy,
		)
	}
	version := ""
	if cfg.Env != nil {
		version = cfg.Env.Build.Version
	}
	handler = APIVersionMiddleware(version)(RequestIDMiddleware(AuthMiddleware()(handler)))
	if cfg.Env != nil {
		s.envMgr = clienv.NewEnvManager(cfg.Env)
		handler = RuntimeMiddleware(s.envMgr, slog.Default())(handler)
	}
	handler = TimeoutMiddleware(120 * time.Second)(handler)
	handler = RecoveryMiddleware(handler)

	httpSrv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Build lifecycle manager with HTTP + GC components.
	mgr := lifecycle.NewLifecycleManager()
	mgr.Register(components.NewHTTPComponent(httpSrv))
	gcSvc := retentiondomain.New(cfg.DB, cfg.VI)
	mgr.Register(components.NewGCComponent(gcSvc, cfg.Retention))

	// Block until SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := mgr.Start(ctx); err != nil {
		return err
	}

	slog.Info("server ready", "port", cfg.Port)
	<-ctx.Done()
	slog.Info("shutting down...")

	// Graceful shutdown: stop components in reverse order.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := mgr.Stop(shutdownCtx); err != nil {
		slog.Error("shutdown", "err", err)
	}

	slog.Info("server stopped")
	return nil
}
