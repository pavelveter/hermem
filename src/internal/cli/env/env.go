// Package env exposes the runtime context shared by every cobra command
// (Env, BuildInfo) plus the JSON-stdin I/O helpers used by every command
// that consumes a request body.
//
// It lives in its own sub-package so the per-group cobra subpackages
// (cli/memory, cli/task, ...) can import it without forming an import
// cycle with the cli/ root orchestrator, which itself depends on the
// groups (cli.NewRootCommand wires group.NewCmd factories).
//
// Import in a sub-package:     `cli "github.com/.../cli/env"` → cli.Env
// Import in cli/ root files:   `clienv "github.com/.../cli/env"` → clienv.Env
package env

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/store"
	"github.com/pavelveter/hermem/src/internal/vector"
)

// Env captures the singleton runtime context. Constructed once in main.go
// and threaded through every cobra command via NewRootCommand(env).
//
// DB, VI, and Worker start nil; EnsureDB() opens them lazily on the
// first command that needs them. Cobra short-circuits PersistentPreRunE
// for `--help`, `-h`, and bare `./hermem` (no subcommand), so those paths
// never touch the database — that's what makes `./hermem --help` print
// usage instead of "db: migrations: ..." when migrations are broken.
type Env struct {
	Ctx       context.Context
	Cfg       *config.Config
	DB        *sql.DB
	VI        core.VectorIndex
	Embedder  core.Embedder
	Extractor core.LLMExtractor
	Reranker  core.Reranker
	Worker    *metrics.AsyncMetricsWorker
	Build     BuildInfo

	// Lazy state uses plain bools (NOT sync.Once/Mutex) because cobra
	// runs PersistentPreRunE / PersistentPostRunE exactly once per
	// process, sequentially on the main goroutine — no concurrent
	// EnsureDB() / Close() caller exists in our tree. Using sync
	// primitives would make vet's copylocks checker flag Env-by-value
	// copies (subcommands take Env by value) and add churn.
	//
	// If you ever spawn a goroutine that touches EnsureDB/Close you
	// must convert these to sync.Once or sync.Mutex + bool.
	initDone  bool
	initErr   error
	closeDone bool
}

// BuildInfo carries ldflags-injected build metadata (passed to Env.Build
// at boot so any command can render a uniform version banner).
type BuildInfo struct {
	Version   string
	BuildDate string
	GitCommit string
}

// Fatal logs f to stderr with a "hermem:" prefix then exits 1. Centralised
// here so main.go doesn't need to import "log"/"os" directly. Matches the
// pre-lazy-init behaviour where Log.Fatalf was used at the same call sites.
func Fatal(f string, args ...any) {
	log.Fatalf("hermem: "+f, args...)
}

// ErrStdinRequired returned by ReadStdin when stdin is a TTY (the user
// didn't pipe anything in). All commands that consume JSON from stdin
// return this so cobra can render a uniform diagnostic.
var ErrStdinRequired = errors.New("stdin required: pipe JSON or run with --help")

// ReadStdin reads + trims stdin. TTY → ErrStdinRequired.
//
// Behaviour matches the pre-cobra cmd.ReadStdin but as an error-returning
// function so cobra's RunE can branch uniformly.
func ReadStdin() (string, error) {
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return "", ErrStdinRequired
	}
	data, _ := io.ReadAll(os.Stdin)
	return strings.TrimSpace(string(data)), nil
}

// DecodeStdin reads JSON from stdin into v via httputil.DecodeStrict and
// returns a structured error on parse failure.
func DecodeStdin(v interface{}) error {
	data, err := ReadStdin()
	if err != nil {
		return err
	}
	return DecodeString(data, v)
}

// DecodeString parses already-read data (e.g. an empty-stdin fallback
// "{}") through the same strict pipeline as DecodeStdin.
func DecodeString(data string, v interface{}) error {
	code, field, msg, ok := httputil.DecodeStrict(strings.NewReader(data), v)
	if !ok {
		if code != "" {
			return fmt.Errorf("invalid request: %s (code=%s field=%s)", msg, code, field)
		}
		return fmt.Errorf("invalid request: %s", msg)
	}
	return nil
}

// WriteJSON encodes data as JSON to w. Centralised so a future move to
// ND-JSON or YAML only has one diff point.
func WriteJSON(w io.Writer, data interface{}) error {
	return json.NewEncoder(w).Encode(data)
}

// EnsureDB lazily opens the SQLite database, runs pending migrations,
// builds the vector index, and starts the metrics worker. Idempotent —
// repeated or concurrent calls return the same (*sql.DB, error). Wired
// into cobra via root.PersistentPreRunE so every DB-needing subcommand
// triggers it transparently without per-command boilerplate.
//
// Cobra skips PersistentPreRunE for `--help` / `-h` / bare `./hermem`,
// so those paths bypass this entirely. PersistentPostRunE on root closes
// the env after the subcommand returns so callers don't need to do it.
func (e *Env) EnsureDB() error {
	if e.initDone {
		return e.initErr
	}
	e.initDone = true
	if e.Cfg == nil {
		e.initErr = errors.New("env: nil Cfg — main.go must construct Env with a valid config before calling EnsureDB")
		return e.initErr
	}
	db, err := store.InitDB(config.ResolveDBPath(e.Cfg.DBPath), e.Cfg.VectorDim)
	if err != nil {
		e.initErr = fmt.Errorf("init db: %w", err)
		return e.initErr
	}
	metrics.InitMetricsDB(db)
	e.Worker = metrics.InitMetricsWorker(db)
	e.DB = db
	e.VI = vector.NewIndex(e.Cfg.VectorBackend, db, e.Cfg.VectorDim)
	return nil
}

// Close drains the metrics worker, then closes the database. Idempotent
// — safe to call multiple times (double defer or via SIGINT cleanup).
// Called from root.PersistentPostRunE so graceful shutdown works even
// though main.go's `defer env.Close()` operates on a value-passed copy.
func (e *Env) Close() {
	if e.closeDone {
		return
	}
	e.closeDone = true
	if e.Worker != nil {
		e.Worker.Stop()
	}
	if e.DB != nil {
		_ = e.DB.Close()
	}
}

// WriteStdout is a package-level EPIPE-tolerant wrapper over os.Stdout.Write.
//
// Cobra's cmd.OutOrStdout() already swallows EPIPE on its own writer, but
// raw os.Stdout writes (and any future stdout caller) need this guard so a
// piped downstream like `hermem ... | head -n 1` doesn't propagate a
// SIGPIPE — that would surface as an ugly stack trace in the consumer's
// terminal. We return nil on EPIPE: the downstream closed, so our work is
// finished; exit 0 is the natural outcome.
func WriteStdout(p []byte) error {
	_, err := os.Stdout.Write(p)
	if errors.Is(err, syscall.EPIPE) {
		return nil
	}
	return err
}
