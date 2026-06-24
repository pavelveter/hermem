// Package cmd hosts every hermem CLI subcommand, dispatching via a flat
// registry that each cmd/<name>.go file populates in its init(). The single
// exported surface for `package main` is Env + Register + Run + Names —
// successful registration of a command name panics so any duplicate panics
// at process start rather than silently shadowing.
package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
)

// BuildInfo carries the ldflags-injected build metadata so any command can
// advertise its version banner without depending on package main globals.
type BuildInfo struct {
	Version   string
	BuildDate string
	GitCommit string
}

// Env is the runtime context passed to every CLI handler. Loaded once in
// main.go and reused across the single dispatch.
type Env struct {
	Ctx       context.Context
	Cfg       *config.Config
	DB        *sql.DB
	VI        core.VectorIndex
	Embedder  core.Embedder
	Extractor core.LLMExtractor
	Reranker  core.Reranker
	Build     BuildInfo
}

// Handler runs a command. Implementations print to stdout, or call log.Fatal
// on unrecoverable error.
type Handler func(env Env)

// registry is built bottom-up by per-file init() functions.
// RWMutex so Register/Run/Names are safe under t.Parallel() in tests even
// though at runtime init() finishes before main() runs.
var (
	mu       sync.RWMutex
	registry = map[string]Handler{}
)

// Register adds a command under name. Duplicates panic — caught at startup,
// not in production.
func Register(name string, h Handler) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("cmd.Register: %q already registered", name))
	}
	registry[name] = h
}

// Run dispatches the named command against env. Returns false when the name
// isn't registered — caller decides how to surface (print usage + exit 1).
func Run(name string, env Env) bool {
	mu.RLock()
	h, ok := registry[name]
	mu.RUnlock()
	if !ok {
		return false
	}
	h(env)
	return true
}

// Names returns every registered name (primary + aliases), alphabetically.
// Used by --help; stable ordering so callers can diff against a snapshot.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
