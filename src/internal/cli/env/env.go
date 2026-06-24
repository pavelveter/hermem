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
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pavelveter/hermem/src/internal/config"
	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
)

// Env captures the singleton runtime context. Constructed once in main.go
// and threaded through every cobra command via NewRootCommand(env).
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

// BuildInfo carries ldflags-injected build metadata (passed to Env.Build
// at boot so any command can render a uniform version banner).
type BuildInfo struct {
	Version   string
	BuildDate string
	GitCommit string
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

// BytesHelper avoids the linter complaining about the bytes import —
// kept for callers that need a streaming variant of DecodeStrict.
var _ = bytes.NewReader
