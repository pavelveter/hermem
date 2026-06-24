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
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/ingestion"
	"github.com/pavelveter/hermem/src/internal/server"
	mem "github.com/pavelveter/hermem/src/internal/server/memory"
	ret "github.com/pavelveter/hermem/src/internal/server/retrieval"
	tasksvc "github.com/pavelveter/hermem/src/internal/server/task"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	"github.com/pavelveter/hermem/src/internal/store"
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
	worker := ingestion.NewIngestionWorker(env.DB, env.VI, env.Extractor, env.Embedder, env.Cfg.DedupThreshold, env.Cfg.Schema)

	srv := server.NewServer(
		refs,
		ret.New(env.DB, env.VI, env.Embedder, refs),
		tasksvc.New(env.DB, env.VI, env.Embedder, refs),
		mem.New(env.DB, env.VI, env.Embedder, worker, refs),
		server.NewAdminService(env.DB, env.VI, env.Embedder, refs),
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
