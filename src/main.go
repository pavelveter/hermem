// Package main is the hermem binary entrypoint — config/DB/vector-index/
// embedder wiring plus build vars; the CLI itself is wired in
// src/internal/cli/.
//
// Build-time variables injected via -ldflags:
//
//	-X main.version=$(VERSION)
//	-X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)
//	-X main.gitCommit=$(git rev-parse --short HEAD)
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/pavelveter/hermem/src/internal/cli"
	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func main() {
	cfg, err := config.LoadConfigFromBinaryDir()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := store.InitDB(config.ResolveDBPath(cfg.DBPath), cfg.VectorDim)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer db.Close()

	metrics.InitMetricsDB(db)
	vi := vector.NewIndex(cfg.VectorBackend, db, cfg.VectorDim)
	mw := metrics.InitMetricsWorker(db)
	defer mw.Stop()

	env := clienv.Env{
		Ctx:       context.Background(),
		Cfg:       cfg,
		DB:        db,
		VI:        vi,
		Embedder:  cfg.NewEmbedder(),
		Extractor: cfg.NewExtractor(),
		Reranker:  cfg.NewReranker(),
		Build: clienv.BuildInfo{
			Version:   version,
			BuildDate: buildDate,
			GitCommit: gitCommit,
		},
	}

	if err := cli.NewRootCommand(env).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
