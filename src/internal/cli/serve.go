package cli

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/config"
	contradictdomain "github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
	edgedomain "github.com/pavelveter/hermem/src/internal/edge"
	graphdomain "github.com/pavelveter/hermem/src/internal/graph"
	healthdomain "github.com/pavelveter/hermem/src/internal/health"
	ingestdomain "github.com/pavelveter/hermem/src/internal/ingest"
	memdomain "github.com/pavelveter/hermem/src/internal/memory"
	migrationdomain "github.com/pavelveter/hermem/src/internal/migration"
	reembeddomain "github.com/pavelveter/hermem/src/internal/reembed"
	retentiondomain "github.com/pavelveter/hermem/src/internal/retention"
	retdomain "github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/server"
	cnd "github.com/pavelveter/hermem/src/internal/server/contradiction"
	edgesrv "github.com/pavelveter/hermem/src/internal/server/edge"
	graphsrv "github.com/pavelveter/hermem/src/internal/server/graph"
	healthsrv "github.com/pavelveter/hermem/src/internal/server/health"
	ingsrv "github.com/pavelveter/hermem/src/internal/server/ingest"
	mem "github.com/pavelveter/hermem/src/internal/server/memory"
	migrsrv "github.com/pavelveter/hermem/src/internal/server/migration"
	reembedsrv "github.com/pavelveter/hermem/src/internal/server/reembed"
	retsrv "github.com/pavelveter/hermem/src/internal/server/retention"
	ret "github.com/pavelveter/hermem/src/internal/server/retrieval"
	tasksvc "github.com/pavelveter/hermem/src/internal/server/task"
	tlsrv "github.com/pavelveter/hermem/src/internal/server/timeline"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	"github.com/pavelveter/hermem/src/internal/store"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
	timelinedomain "github.com/pavelveter/hermem/src/internal/timeline"
	"github.com/pavelveter/hermem/src/internal/util/safego"
)

// newServeCmd boots the HTTP server. Replaces the old flat `hermem serve`.
// Port is a real cobra flag (--port/-p, default 8420) — no positional arg.
func newServeCmd(env *clienv.Env) *cobra.Command {
	var port string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server (default :8420)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(env, port)
		},
	}
	cmd.Flags().StringVarP(&port, "port", "p", "8420", "HTTP port to listen on")
	return cmd
}

func runServe(env *clienv.Env, port string) error {
	slog.Info("hermem starting",
		"port", port,
		"version", env.Build.Version,
		"build_date", env.Build.BuildDate,
		"git_commit", env.Build.GitCommit,
	)

	refs := serverstate.NewRef(buildState(env.Cfg, env.Reranker))
	// PHASE 2.1: build the memory domain Service once and hand the HTTP shell
	// (server/memory) a borrowed pointer. IngestionWorker was created
	// inside Mem.Ingest per call pre-PHASE-3.4 (now constructed inside
	// ingest.Service.Ingest). Embedder lives inside memdomain.Service;
	// Extractor remains in memdomain for any future memory-write hook
	// though unused since PHASE 3.4.
	memSvc := memdomain.New(env.DB, env.VI, env.Embedder, env.Extractor)
	// PHASE 3.5: edge domain Service owns the relation-edge write API
	// (AddEdge + auto-create dispatch). vi + embedder are held so the
	// auto-create path can call vector.AddEdgeWithAutoCreate without
	// constructor branching on each invocation.
	edgeSvc := edgedomain.New(env.DB, env.VI, env.Embedder)
	// PHASE 3.5: timeline domain Service owns the read-only time-ordered
	// entity projection. db-only — no embedder, no vector index, no
	// schema gates (read surface, not write surface).
	timelineSvc := timelinedomain.New(env.DB)
	// PHASE 3.6: reembed domain Service owns the batch re-embedding
	// orchestrator moved from algo/reembed.go (deleted in this phase).
	// The three deps (db, vi, embedder) are Service fields; per-call
	// args (dim, batchSize, model) come from the request.
	healthSvc := healthdomain.New(
		healthdomain.DBProbe(env.DB),
		healthdomain.VectorIndexProbe(env.VI, env.Cfg.VectorDim),
		healthdomain.EmbedderProbe(env.Embedder),
		healthdomain.ExtractorProbe(env.Extractor),
		healthdomain.DiskSpaceProbe(env.Cfg.DBPath),
	)
	reembedSvc := reembeddomain.New(env.DB, env.VI, env.Embedder)
	// PHASE 2.2: same shape for retrieval. The domain Service owns
	// retrieval orchestration; HTTP shell delegates through RetSvc.
	retSvc := retdomain.NewService(env.DB, env.VI, env.Embedder)
	// PHASE 2.3: contradiction domain Service is read-only and
	// DB-only (no vector index / embedder / schema); same shape as
	// retrieval/memory but slimmer dependencies.
	cndSvc := contradictdomain.NewService(env.DB)
	// PHASE 2.4: task domain Service holds db + embedder + vi. The
	// embedded AutoLinkEdges inside Service.Create is the only call
	// path that uses embedder + vi; all other methods are pure SQL.
	// The HTTP shell (server/task) takes a borrowed pointer to this
	// Service and threads it into the 10-endpoint mux.
	taskSvc := taskdomain.NewService(env.DB, env.Embedder, env.VI)
	// PHASE 3.1: graph domain Service is read-only and DB-only
	// (same shape as contradiction's PHASE 2.3 precedent). The
	// HTTP shell mounts /connected-components + /communities
	// (moved from AdminService) plus the NEW /graph/verify. Dim
	// is loaded once from cfg at boot — VectorDim is a static
	// dimensional commitment for the lifetime of the daemon.
	graphSvc := graphdomain.NewService(env.DB)
	// PHASE 3.2: migration domain Service covers schema / migration
	// inspection (db/migrate / db/rollback / db/verify / db/schema).
	// OUT OF SCOPE: store.RunMigrations + store.StoreSchemaFingerprint
	// stay in store/ — they are bootstrapping mutating hooks called
	// from main.go boot and cli/serve.go's SIGHUP loop, not
	// request-time reads. The HTTP shell exposes 4 NEW routes that
	// previously had no HTTP surface (only CLI subcommands).
	migrSvc := migrationdomain.NewService(env.DB)
	// PHASE 3.4: ingest domain Service owns the synchronous dialog
	// pipeline orchestration (extraction -> embed -> dedup -> upsert
	// -> edges). Constructs an IngestionWorker PER CALL inside
	// Ingest() — preserves the pre-PHASE-2.1 SIGHUP-race-free invariant.
	// The HTTP shell replaces the previously-on-server/memory-shell
	// HandleIngest route; the URL stays at /ingest. /ingest/jobs GET
	// endpoint is NEW.
	ingestSvc := ingestdomain.NewService(env.DB, env.VI, env.Embedder, env.Extractor)
	// PHASE 3.3: retention domain Service owns the archive sweep
	// (RunOnce + Run loop). DefaultPolicy is captured at construction
	// from cfg.Retention and passed to the HTTP shell; SIGHUP does not
	// propagate policy changes (matches pre-PHASE-3.3 closure-capture
	// behaviour inside server.Server.Serve). The long-lived Run
	// goroutine is wired by server.Server.Serve directly — cli/serve.go
	// is only responsible for constructing the domain Service + HTTP
	// shell here.
	// NOTE: variable name `retentionSvc` (NOT `retSvc`) to avoid a
	// name collision with the retrieval domain Service declared
	// further up; both have the type prefix `*Service` so a single
	// short name would shadow.
	retentionSvc := retentiondomain.NewService(env.DB, env.VI)

	srv := server.NewServer(
		refs,
		ret.New(retSvc, env.Metrics, refs),
		tasksvc.New(taskSvc, env.Metrics, refs),
		mem.New(memSvc, env.Metrics, refs, env.Cfg.DedupThreshold),
		edgesrv.New(edgeSvc, env.Metrics, refs),
		tlsrv.New(timelineSvc, env.Metrics),
		ingsrv.New(ingestSvc, env.Metrics, refs, env.Cfg.DedupThreshold),
		cnd.New(cndSvc, env.Metrics),
		graphsrv.New(graphSvc, env.Metrics, refs, env.Cfg.VectorDim),
		migrsrv.New(migrSvc, env.Metrics, refs),
		retsrv.New(retentionSvc, env.Metrics, refs, env.Cfg.Retention),
		reembedsrv.New(reembedSvc, env.Metrics),
		healthsrv.New(healthSvc),
		env.Metrics,
	)

	// SIGHUP reload loop — separate from HTTP lifecycle so we can re-validate
	// config without restarting. srv.ReloadState atomically swaps the State
	// via serverstate.Ref (atomic.Pointer) — concurrent handlers always read a
	// consistent snapshot through s.Refs.Load(). No additional synchronisation
	// is needed between this goroutine and HTTP handler goroutines.
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	// Wrap the SIGHUP loop in safego.Go so a panic during config reload
	// (e.g. a buggy validator) cannot crash the daemon mid-loop. The
	// goroutine's lifetime is the parent env.Ctx; signal.Receive stops
	// when ctx is cancelled, so the inner for-range drains on shutdown.
	safego.Go(env.Ctx, "sighup-reload", func(_ context.Context) {
		for range sighup {
			newCfg, err := config.LoadConfigFromBinaryDir()
			if err != nil {
				slog.Error("SIGHUP: load config", "err", err)
				continue
			}
			if err := newCfg.Validate(); err != nil {
				slog.Error("SIGHUP: invalid config", "err", err)
				continue
			}
			srv.ReloadState(buildState(newCfg, newCfg.NewReranker()))
			_ = store.StoreSchemaFingerprint(env.DB, newCfg.Schema)
			slog.Info("SIGHUP applied")
		}
	})

	return srv.Serve(server.ServeConfig{
		DB:        env.DB,
		VI:        env.VI,
		Retention: env.Cfg.Retention,
		APIKey:    env.Cfg.APIKey,
		Port:      port,
	})
}

// buildState constructs a *serverstate.State from a config + Reranker.
// Used at boot AND inside the SIGHUP loop — same shape as pre-cobra serve.
func buildState(cfg *config.Config, reranker core.Reranker) *serverstate.State {
	cats := cfg.Schema.AllowedCategories
	if cats == nil {
		cats = map[string]bool{}
	}
	rels := cfg.Schema.AllowedRelations
	if rels == nil {
		rels = map[string]bool{}
	}
	return &serverstate.State{
		Schema:             cfg.Schema,
		ValidCategories:    cats,
		ValidRelationTypes: rels,
		RankingWeight:      cfg.Ranking,
		Reranker:           reranker,
		DepthCeiling:       cfg.MaxDepthCeiling,
		MaxRetrievedNodes:  cfg.MaxRetrievedNodes,
	}
}
