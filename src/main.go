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
	"errors"
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

// signalInit ignores SIGPIPE so a piped downstream consumer closing
// early (y | head -n 1) does NOT generate a Go stack-trace at process
// exit. After Ignore, the next os.Stdout.Write call returns EPIPE
// instead of being terminated by the signal; clienv.WriteStdout then
// maps that error to a nil return so the CLI exits 0 cleanly with the
// partial output already written.
//
// The Ignore call is process-wide; any future code that re-arms a
// SIGPIPE handler (e.g. an http.Server that wants SIGPIPE logging)
// must do so explicitly via signal.Notify.
func signalInit() {
	signal.Ignore(syscall.SIGPIPE)
}

// signalExitCode returns 130 if the SIGINT/SIGTERM-triggered ctx is
// cancelled, else zero. Used by main() to map signal cancellation to
// the conventional SIGINT exit code (so shell wrappers can distinguish
// "user Ctrl-C" from "command succeeded" via exit-status inspection).
//
// 130 follows POSIX shellscript convention (128 + signal-number 2 for
// SIGINT). SIGTERM would yield 143; we don't distinguish here because
// both go through the same NotifyContext path and the operator's main
// concern is "did a user-or-supervisor kill mid-flight" rather than
// which exact signal did it.
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
		// Distinguish user/SIGINT/SIGTERM-triggered cancellations from
		// every other cobra error so the operator sees exit 130 (signal)
		// vs exit 1 (cobra error) in shell wrappers. clienv.Fatal below
		// bails the process via log.Fatalf — run only for non-signal
		// failures.
		//
		// env.Close() runs BEFORE os.Exit because os.Exit does NOT run
		// deferred functions in Go — without this explicit Close call,
		// SIGINT-triggered exits would leak the SQLite *sql.DB handle
		// (worker.Stop + db.Close inside env.Close) and leave metrics
		// unflushed. env.Close is idempotent (sync.Once safe) so the
		// second Close via defer never reopens or double-closes anything.
		if code := signalExitCode(ctx); code != 0 {
			env.Close()
			os.Exit(code)
		}
		env.Close()
		clienv.Fatal("%v", err)
	}
	// No cobra error but signal-driven ctx cancellation may have
	// landed AFTER parse and BEFORE Execute returned. Promote to 130
	// for shell-wrapper consistency (otherwise a CLI invocation
	// cancelled between parse and dispatch would silently exit 0).
	//
	// Run env.Close explicitly before os.Exit for the same reason as
	// above — defer doesn't fire after os.Exit.
	if code := signalExitCode(ctx); code != 0 {
		env.Close()
		os.Exit(code)
	}
}
