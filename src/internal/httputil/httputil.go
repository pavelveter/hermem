// Package httputil provides HTTP helpers shared by all server sub-packages.
//
// These helpers used to live inside the server package and were called via
// short names (WriteJSON, DecodeStrict, parseIntParam). They are extracted
// here so the new retrieval/task/memory/admin services can use them without
// importing the server god-object package.
//
// Package prefix change is intentional and breaking by design — callers
// must update to `httputil.WriteJSON(...)` etc. alongside the server split.
package httputil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/pavelveter/hermem/src/internal/core"
)

// MaxBodyBytes caps request bodies on local POST handlers.
// Services compose with http.MaxBytesReader(w, r.Body, MaxBodyBytes).
const MaxBodyBytes = 1 << 20 // 1 MiB

// WriteJSON encodes data as JSON and writes it with the given status code.
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, core.ErrorResponse{Error: msg})
}

// WriteErrorWithCode writes a structured error with code and field.
func WriteErrorWithCode(w http.ResponseWriter, status int, msg, code, field string) {
	WriteJSON(w, status, core.ErrorResponse{Error: msg, Code: code, Field: field})
}

// DecodeStrict parses JSON while rejecting unknown fields and trailing data.
// Returns (code, field, msg, ok). On success, returns ("", "", "", true).
//
// CONTRACT: the caller is responsible for r.Body lifecycle. After this
// function returns (success or any error path) the caller MUST drain any
// unread bytes from r.Body and close it. The SafeBodyCloseMiddleware in
// the server package enforces this on the success / explicit-return
// paths, but custom entry points (CLI commands, tests, integration
// adapters) MUST mirror the pattern:
//
//	defer func() {
//	    _, _ = io.Copy(io.Discard, r.Body)
//	    _ = r.Body.Close()
//	}()
//
// Failure to drain leaks the connection into CLOSE_WAIT; failure to
// Close compounds it. The MaxBytesReader cap on r.Body returns a
// MaxBytesError on overflow — callers should NOT retry the same body
// after that error.
func DecodeStrict(r io.Reader, dst interface{}) (code, field, msg string, ok bool) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
		if dec.More() {
			return "trailing_data", "", "trailing data after JSON value", false
		}
		return "", "", "", true
	}
	if errors.Is(err, io.EOF) {
		return "empty_body", "", "request body is empty", false
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return "invalid_type", typeErr.Field, fmt.Sprintf("invalid type for field %q", typeErr.Field), false
	}
	if strings.HasPrefix(err.Error(), "json: unknown field") {
		fn := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return "unknown_field", fn, "unknown field: " + fn, false
	}
	return "bad_json", "", "invalid json: " + err.Error(), false
}

// ParseIntParam reads an int query parameter with a default.
// Returns def if the param is missing or unparseable.
func ParseIntParam(r *http.Request, name string, def int) int {
	if s := r.URL.Query().Get(name); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}
