package cmd

import (
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/server"
	"github.com/pavelveter/hermem/src/internal/store"
)

func init() { Register("serve", cliServe) }

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

	srv := server.NewServer(env.DB, env.VI, env.Embedder, env.Extractor, env.Cfg.DedupThreshold,
		core.RetrieveContextOptions{
			DepthCeiling:      env.Cfg.MaxDepthCeiling,
			MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
			RankingWeight:     env.Cfg.Ranking,
			Reranker:          env.Reranker,
		},
		env.Cfg.Schema)

	// SIGHUP reload loop — separate from server lifecycle so we can re-validate
	// config without restarting.
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
			srv.ReloadState(newCfg.Schema, newCfg.Ranking, newCfg.NewReranker())
			_ = store.StoreSchemaFingerprint(env.DB, newCfg.Schema)
			slog.Info("SIGHUP applied")
		}
	}()

	if err := server.StartStandalone(server.StartStandaloneConfig{
		DB:                env.DB,
		VI:                env.VI,
		Embedder:          env.Embedder,
		Extractor:         env.Extractor,
		Reranker:          env.Reranker,
		Schema:            env.Cfg.Schema,
		Ranking:           env.Cfg.Ranking,
		DedupThreshold:    env.Cfg.DedupThreshold,
		DepthCeiling:      env.Cfg.MaxDepthCeiling,
		MaxRetrievedNodes: env.Cfg.MaxRetrievedNodes,
		Retention:         env.Cfg.Retention,
		APIKey:            env.Cfg.APIKey,
		Port:              port,
	}); err != nil {
		log.Fatalf("server: %v", err)
	}
}
