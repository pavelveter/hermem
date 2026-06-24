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
	"sync/atomic"
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

	// KeepDBOpen disables auto-closing the database in
	// root.PersistentPostRunE. Default is false (production behaviour:
	// env closes after each `$ hermem <cmd>` returns, so a shell
	// invocation does not leave an orphan file handle). Tests set this
	// to true so they can run multiple executeCmd calls against the
	// same env within one test body; tests rely on t.Cleanup(env.Close)
	// for final teardown. Hot-reloaded Envs propagate the field via
	// EnvManager.Reload so a reload mid-test doesn't accidentally
	// re-enable auto-close.
	KeepDBOpen bool

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

// EnvManager holds the current *Env behind an atomic.Pointer so a hot
// reload (SIGHUP, config file change, admin "reload" RPC) cannot expose
// a half-mutated snapshot to concurrent readers.
//
// Use this instead of a package-global `var CurrentEnv *Env` — the global
// version is the bug 11.1 calls out: a reader can fetch a pointer that
// has been partially overwritten by a writer, producing impossible-to-debug
// shape mismatches. atomic.Pointer.Load() returns a consistent snapshot.
//
// Construction:
//
//	em := NewEnvManager(env)
//	em.Get()         // readers
//	em.Set(newEnv)   // writers (admin only)
//	em.Reload(cfg)   // writers that go through the validator
type EnvManager struct {
	current atomic.Pointer[Env]
}

// NewEnvManager constructs an EnvManager that hands out `initial` until
// the first Reload/Set call. Safe to call with a nil `initial` — Get()
// returns nil in that case, callers should treat nil as "not ready".
func NewEnvManager(initial *Env) *EnvManager {
	m := &EnvManager{}
	if initial != nil {
		m.current.Store(initial)
	}
	return m
}

// Get returns the latest snapshot of *Env. Readers may call this from
// any goroutine; concurrent Store/Reload calls are atomic from the
// reader's point of view.
func (m *EnvManager) Get() *Env {
	return m.current.Load()
}

// Set unconditionally stores env. Admin-only path — bypasses cfg.Validate.
// Production hot-reload should call Reload, which validates first.
func (m *EnvManager) Set(env *Env) {
	m.current.Store(env)
}

// Reload validates cfg, then atomically swaps a freshly-built *Env in
// place. Returns error without mutating Env when validation fails so a
// bad config cannot half-reload the server.
//
// On swap, all open handles (DB, VI, Embedder, Extractor, Reranker,
// Worker) and the init/close bookkeeping from the prior *Env are
// carried forward — only `Cfg` is replaced. Dropping those fields
// would zero them, and any downstream caller that did
// `em.Get().DB` would dereference nil and crash the daemon.
func (m *EnvManager) Reload(cfg *config.Config) (*Env, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("env reload: validate: %w", err)
	}
	prev := m.Get()
	newEnv := &Env{
		Cfg:        cfg,
		Build:      safeGet(prev, func(e *Env) BuildInfo { return e.Build }),
		Ctx:        safeGet(prev, func(e *Env) context.Context { return e.Ctx }),
		DB:         safeGet(prev, func(e *Env) *sql.DB { return e.DB }),
		VI:         safeGet(prev, func(e *Env) core.VectorIndex { return e.VI }),
		Embedder:   safeGet(prev, func(e *Env) core.Embedder { return e.Embedder }),
		Extractor:  safeGet(prev, func(e *Env) core.LLMExtractor { return e.Extractor }),
		Reranker:   safeGet(prev, func(e *Env) core.Reranker { return e.Reranker }),
		Worker:     safeGet(prev, func(e *Env) *metrics.AsyncMetricsWorker { return e.Worker }),
		KeepDBOpen: safeGet(prev, func(e *Env) bool { return e.KeepDBOpen }),
		initDone:   safeGet(prev, func(e *Env) bool { return e.initDone }),
		initErr:    safeGet(prev, func(e *Env) error { return e.initErr }),
		closeDone:  safeGet(prev, func(e *Env) bool { return e.closeDone }),
	}
	m.current.Store(newEnv)
	return newEnv, nil
}

// safeGet returns the zero value of T when prev is nil, otherwise it
// returns extractor(prev). Replaces the dozen near-identical per-field
// accessors (copyBuild, prevCtx, prevDB, prevEmbedder, …) previously
// hand-written below — the boilerplate was identical (nil-check + one
// field read) and adding a new state field required another ~5-line
// function of mechanical duplication.
//
// Used by EnvManager.Reload to copy state from the prior *Env onto the
// newly-built one without dereferencing a nil receiver when the manager
// starts empty (see NewEnvManager(nil)). Callers that want to mutate an
// open handle (e.g. reinitialise the DB on schema drift) should call
// Set() instead of Reload.
//
// The helper is intentionally unexported: the pattern is local to
// EnvManager.Reload, no other package needs it, and lowercase keeps the
// most common use (zero-value-on-nil task) inside this file. Name is
// lowercase "s" so it doesn't collide with any future public Get helper.
func safeGet[T any](prev *Env, extractor func(*Env) T) T {
	var zero T
	if prev == nil {
		return zero
	}
	return extractor(prev)
}
