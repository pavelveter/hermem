// Package main is the hermem binary entrypoint — eager DI via
// app.Application plus build vars; the CLI itself lives under
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
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/pavelveter/hermem/src/internal/app"
	"github.com/pavelveter/hermem/src/internal/cli"
	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/config"
)

var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func signalInit() {
	signal.Ignore(syscall.SIGPIPE)
}

func signalExitCode(ctx context.Context) int {
	if errors.Is(ctx.Err(), context.Canceled) {
		return 130
	}
	return 0
}

func main() {
	cfg, err := config.LoadConfigFromBinaryDir()
	if err != nil {
		clienv.Fatal("config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		clienv.Fatal("config: %v", err)
	}

	signalInit()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Construct the typed DI container — ALL dependencies are eager,
	// non-nil, and wired in deterministic order. No lazy EnsureDB.
	buildInfo := app.BuildInfo{
		Version:   version,
		BuildDate: buildDate,
		GitCommit: gitCommit,
	}
	a, err := app.New(ctx, cfg, buildInfo)
	if err != nil {
		clienv.Fatal("app: %v", err)
	}
	defer func() { _ = a.Stop(context.Background()) }()

	// Convert to *clienv.Env for backward compatibility with CLI
	// commands that still accept *clienv.Env. This adapter will be
	// removed once all commands are migrated to *app.Application.
	env := applicationToEnv(a, ctx)

	if err := cli.NewRootCommand(env).Execute(); err != nil {
		if code := signalExitCode(ctx); code != 0 {
			_ = a.Stop(context.Background())
			os.Exit(code)
		}
		_ = a.Stop(context.Background())
		clienv.Fatal("%v", err)
	}
	if code := signalExitCode(ctx); code != 0 {
		_ = a.Stop(context.Background())
		os.Exit(code)
	}
}

// applicationToEnv converts an *app.Application to a *clienv.Env for
// backward compatibility with CLI commands. This is a transitional
// adapter — it will be removed once all commands accept *app.Application
// directly.
func applicationToEnv(a *app.Application, ctx context.Context) *clienv.Env {
	return &clienv.Env{
		Ctx:       ctx,
		Cfg:       a.Cfg,
		DB:        a.DB,
		VI:        a.VI,
		Embedder:  a.Embedder,
		Extractor: a.Extractor,
		Reranker:  a.Reranker,
		Retriever: a.Retriever,
		Metrics:   a.Metrics,
		Worker:    a.Worker,
		Tracer:    a.Tracer,
		Build: clienv.BuildInfo{
			Version:   a.Build.Version,
			BuildDate: a.Build.BuildDate,
			GitCommit: a.Build.GitCommit,
		},
		KeepDBOpen: true, // DB lifecycle managed by app.Application.Stop()
	}
}
