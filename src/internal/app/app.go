// Package app provides the typed DI container for hermem.
//
// Application constructs ALL dependencies eagerly — no nil fields, no
// lazy init, no temporal coupling. The dependency graph is explicit in
// one struct and the lifecycle (Start/Stop) is ordered in code.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/tracing"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// BuildInfo holds build-time metadata injected via -ldflags.
type BuildInfo struct {
	Version   string
	BuildDate string
	GitCommit string
}

// Application is the typed DI container for hermem. All fields are
// non-nil after New() returns. The struct owns the data layer (DB,
// VI, domain services) and the worker lifecycle. The HTTP serving
// layer consumes Application but manages its own server lifecycle
// (including SIGHUP hot-reload) in serve.go.
type Application struct {
	DB        *sql.DB
	VI        core.VectorIndex
	Worker    *metrics.AsyncMetricsWorker
	Embedder  core.Embedder
	Extractor core.LLMExtractor
	Reranker  core.Reranker
	Retriever core.Retriever
	Metrics   *metrics.Metrics
	Tracer    tracing.Tracer
	Cfg       *config.Config
	Build     BuildInfo

	stopOnce sync.Once
}

// New constructs an Application with ALL dependencies eagerly
// initialized. No field is nil on return. Returns an error if DB
// initialization fails (bad path, corrupt schema, pending migrations
// without autoMigrate).
//
// New does NOT start background workers — call Start() after
// construction to begin async operations.
func New(_ context.Context, cfg *config.Config, build BuildInfo) (*Application, error) {
	// --- AI clients (no DB needed) ---
	embedder := cfg.NewEmbedder()
	extractor := cfg.NewExtractor()
	reranker := cfg.NewReranker()

	// --- Database ---
	dbPath := config.ResolveDBPath(cfg.DBPath)
	db, err := store.InitDBStrictWithOptions(dbPath, cfg.VectorDim, cfg.AutoMigrate, false)
	if err != nil {
		return nil, fmt.Errorf("app: open db: %w", err)
	}

	// --- Metrics infrastructure ---
	metrics.InitMetricsDB(db)
	worker := metrics.InitMetricsWorker(db)
	m := metrics.New()

	// --- Vector index ---
	vi := vector.NewIndex(cfg.VectorBackend, db, cfg.VectorDim)

	// --- Tracer ---
	tracer := tracing.NewTracerFromEnv()

	// --- Retrieval service (exposed as core.Retriever interface) ---
	retSvc := retrieval.New(db, vi, embedder)

	return &Application{
		DB:        db,
		VI:        vi,
		Worker:    worker,
		Embedder:  embedder,
		Extractor: extractor,
		Reranker:  reranker,
		Retriever: retSvc,
		Metrics:   m,
		Tracer:    tracer,
		Cfg:       cfg,
		Build:     build,
	}, nil
}

// Start begins background operations (metrics worker flush loop).
// Must be called after New(). The provided ctx controls the lifetime
// of background goroutines — when ctx is cancelled, workers should
// drain and exit.
func (a *Application) Start(_ context.Context) error {
	// Worker is already started by metrics.InitMetricsWorker;
	// Start exists for symmetry with Stop and to allow future
	// lazy-start patterns (e.g. starting a garbage collector
	// goroutine) without breaking the caller.
	return nil
}

// Stop gracefully shuts down background resources in reverse
// initialization order: worker drain → DB close. Safe to call
// multiple times (sync.Once).
func (a *Application) Stop(_ context.Context) error {
	var stopErr error
	a.stopOnce.Do(func() {
		if a.Worker != nil {
			a.Worker.Stop()
		}
		if a.DB != nil {
			stopErr = a.DB.Close()
		}
	})
	return stopErr
}
