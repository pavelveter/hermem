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
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/pavelveter/hermem/src/internal/app"
	"github.com/pavelveter/hermem/src/internal/cli"
	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/metrics"
)

var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

// signalInit ignores SIGPIPE so that writing to a closed pipe (e.g. hermem
// serve | head) exits cleanly instead of crashing with EPIPE.
//
// IMPORTANT: signal.Ignore is process-wide. Any code that calls
// signal.Notify for a different signal must not rely on SIGPIPE being
// un-ignored — it will be. This call lives in main() because it must
// run exactly once before any goroutine writes to stdout/stderr.
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
	// Parse --config flag early (before cobra) so config path is available.
	// Precedence (flag > env > binary-dir) is enforced inside
	// LoadConfigFromSources — main.go only hands off the parsed flag
	// value, so a future caller can swap the parser without re-implementing
	// the precedence rules.
	configPath := flag.String("config", "", "path to hermem.ini (overrides HERMEM_INI env and binary-dir default)")
	_ = flag.CommandLine.Parse(os.Args[1:])

	cfg, err := config.LoadConfigFromSources(*configPath)
	if err != nil {
		clienv.Fatal("config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		clienv.Fatal("config: %v", err)
	}

	signalInit()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Fast path: pure-output leaves don't need DB / vector index / AI
	// clients / async workers. Identify the leaf BEFORE app.New so we
	// don't pay the cost of `store.InitDBStrictWithOptions` (which fails
	// with `app: open db: schema has pending migrations` on a fresh CI
	// scratch DB with no hermem.ini — the case `release.yml`'s Build
	// matrix tripped on `./hermem completion bash`). The full DI graph
	// stays intact for the normal path; we just don't construct it when
	// the leaf is a known pure-output reader of env.Build / env.Metrics
	// only.
	if leaf := firstLeafArg(flag.Args()); noDBLeaves[leaf] {
		env := noDBEnv(ctx, cfg)
		if err := cli.NewRootCommand(env).Execute(); err != nil {
			if code := signalExitCode(ctx); code != 0 {
				os.Exit(code)
			}
			clienv.Fatal("%v", err)
		}
		if code := signalExitCode(ctx); code != 0 {
			os.Exit(code)
		}
		return
	}

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

// noDBLeaves is the closed set of cobra leaf commands that don't need
// the SQLite database, vector index, AI clients, or async workers.
// Inspected by main() BEFORE app.New() so a no-DB invocation skips the
// eager DI graph entirely. Mirrors the skip_db_<leaf> annotations
// also written by `cli.noopPreRun` (root.go, defence-in-depth — cobra's
// UP-walk hooks will still execute for any future no-DB leaf that
// forgets to register here, but the lifecycle order matters less once
// app.Application isn't constructed at all).
//
// Hardcoded rather than auto-discovered: the set is small, stable,
// and the gate exists to shield a known CI failure mode rather than
// a generic optimisation.
var noDBLeaves = map[string]bool{
	"completion": true, // ./hermem completion bash|zsh|fish — Cobra internal generators only
	"version":    true, // ./hermem version — env.Build only
	"metrics":    true, // ./hermem metrics — env.Metrics.WriteExposition only
	"docs":       true, // ./hermem docs <dir> — Cobra doc package only
	"bench":      true, // ./hermem bench — env.Metrics only (worker / DB unused)
	"help":       true, // ./hermem help [<sub>] — Cobra auto-registered help command
	"__complete": true, // Cobra HIDDEN dynamic-completion command invoked by generated bash completion scripts on TAB-press
}

// firstLeafArg returns the first positional non-flag argument from a
// post-`flag.CommandLine.Parse` slice. Empty string if the slice is empty.
// `flag.Args()` already strips the binary name and known flags (`--config`),
// returning only the trailing positional argv. Index 0 is the leaf in
// all current hermem invocations because cobra's tree is single-level
// for the no-DB leaves (e.g. `./hermem completion bash`, not `./hermem grp cmd`).
func firstLeafArg(positional []string) string {
	if len(positional) == 0 {
		return ""
	}
	return positional[0]
}

// noDBEnv builds a minimal *clienv.Env for pure-output leaves. DB, VI,
// AI clients, Worker, Tracer, and Retriever are deliberately nil — the
// no-DB leaves don't touch them. Metrics is constructed lazily in
// metrics.New() so `./hermem metrics` / `./hermem bench` have an
// in-process registry to write to; other leaves ignore it.
//
// cfg is preserved so commands like `./hermem version` (future) can
// read config metadata without the cost of a DB connection.
func noDBEnv(ctx context.Context, cfg *config.Config) *clienv.Env {
	return &clienv.Env{
		Ctx:     ctx,
		Cfg:     cfg,
		Metrics: metrics.New(),
		Build: clienv.BuildInfo{
			Version:   version,
			BuildDate: buildDate,
			GitCommit: gitCommit,
		},
		// DB, VI, Embedder, Extractor, Reranker, Worker, Retriever, Tracer: nil.
		// Plus initDone=false (no mock EnsureDB) so any DB-needing subcommand
		// added later in a noDBLeaves sibling would fail-fast with the
		// existing `env: nil Cfg` / `nil receiver` guardrails rather than
		// silently fall through.
		KeepDBOpen: false,
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
