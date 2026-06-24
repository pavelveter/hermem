// Package main is the hermem binary entrypoint — config + lazy runtime
// wiring plus build vars; the CLI itself lives under src/internal/cli/.
//
// Build-time variables injected via -ldflags:
//
//	-X main.version=$(VERSION)
//	-X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)
//	-X main.gitCommit=$(git rev-parse --short HEAD)
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/pavelveter/hermem/src/internal/cli"
	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/config"
)

var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func main() {
	cfg, err := config.LoadConfigFromBinaryDir()
	if err != nil {
		clienv.Fatal("config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		clienv.Fatal("config: %v", err)
	}

	// Embedder / Extractor / Reranker don't need the DB — build them
	// eagerly so they are ready when EnsureDB later constructs the
	// server. DB / VI / Worker start nil and are populated lazily by
	// env.EnsureDB(), called from cobra's root PersistentPreRunE.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	env := clienv.Env{
		Ctx:       ctx,
		Cfg:       cfg,
		DB:        nil,
		VI:        nil,
		Embedder:  cfg.NewEmbedder(),
		Extractor: cfg.NewExtractor(),
		Reranker:  cfg.NewReranker(),
		Build: clienv.BuildInfo{
			Version:   version,
			BuildDate: buildDate,
			GitCommit: gitCommit,
		},
	}
	// Defer env.Close() — handles both db.Close and worker.Stop.
	// env.Close is sync.Once idempotent so double-defer is safe.
	defer env.Close()

	if err := cli.NewRootCommand(&env).Execute(); err != nil {
		clienv.Fatal("%v", err)
	}
}
