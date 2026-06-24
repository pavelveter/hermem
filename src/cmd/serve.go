package cmd

import (
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/ingestion"
	"github.com/pavelveter/hermem/src/internal/server"
	mem "github.com/pavelveter/hermem/src/internal/server/memory"
	ret "github.com/pavelveter/hermem/src/internal/server/retrieval"
	tasksvc "github.com/pavelveter/hermem/src/internal/server/task"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	"github.com/pavelveter/hermem/src/internal/store"
)

func init() { Register("serve", cliServe) }

// buildState constructs a *serverstate.State from a config + Reranker.
// Used twice: once at boot, once per SIGHUP.
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

func cliServe(env Env) {
	port := "8420"
	args := argTail()
	if len(args) > 0 {
		port = args[0]
	}

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
	// and propagates to memory.OnStateChange (which calls Worker.ReloadSchema).
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
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
	}()

	if err := srv.Serve(server.ServeConfig{
		DB:        env.DB,
		VI:        env.VI,
		Retention: env.Cfg.Retention,
		APIKey:    env.Cfg.APIKey,
		Port:      port,
	}); err != nil {
		log.Fatalf("server: %v", err)
	}
}
