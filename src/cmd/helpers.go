package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"

	"github.com/pavelveter/hermem/src/internal/server"
)

// --- stdin helpers ---

// ReadStdin reads stdin and trims trailing whitespace. Fails fast on a
// character device (TTY) so callers that piped `{}` get a clean empty body
// but interactive sessions see a clear "stdin required" exit.
func ReadStdin() string {
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		log.Fatal("stdin required")
	}
	data, _ := io.ReadAll(os.Stdin)
	return strings.TrimSpace(string(data))
}

// DecodeStdin reads JSON from stdin into v using server.DecodeStrict — so
// trailing data, unknown fields, and type mismatches all surface from one
// source rather than being re-implemented per command.
func DecodeStdin(v interface{}) {
	if code, field, msg, ok := server.DecodeStrict(bytes.NewReader([]byte(ReadStdin())), v); !ok {
		log.Fatalf("invalid request: %s (code=%s field=%s)", msg, code, field)
	}
}

// DecodeString strings.NewReader(data) + DecodeStrict for cases where the
// caller has already pre-read stdin and may substitute a default body when
// the pipe was empty (e.g. cmd/task-executable falling back to "{}").
func DecodeString(data string, v interface{}) {
	if code, field, msg, ok := server.DecodeStrict(strings.NewReader(data), v); !ok {
		log.Fatalf("invalid request: %s (code=%s field=%s)", msg, code, field)
	}
}

// --- output helpers ---

// writeJSON encodes data to w with the standard encoder. Centralised so an
// eventual logger-style reformat change touches one site.
func writeJSON(w io.Writer, data interface{}) error {
	return json.NewEncoder(w).Encode(data)
}

// --- arg helpers ---

// argTail returns os.Args[2:] (everything after the command name). Used by
// commands that consume CLI flags rather than JSON stdin.
func argTail() []string {
	if len(os.Args) < 3 {
		return nil
	}
	return os.Args[2:]
}
