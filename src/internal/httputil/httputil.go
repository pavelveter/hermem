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
//
// Declared as a var (not const) so tests can temporarily shrink the
// cap to a small byte-value for MaxBytes-overflow assertions without
// allocating a real 1 MiB body. Typed explicitly as int64 to match
// http.MaxBytesReader's parameter type so the ~30 handler call sites
// can pass MaxBodyBytes directly without an int64() cast. Production
// callers treat this as a constant — assignments after package init
// are unexpected.
var MaxBodyBytes int64 = 1 << 20 // 1 MiB

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

// MapError converts a domain error to (HTTP status code, message).
// It unwraps *core.DomainError to map machine-readable error codes
// to HTTP statuses. Non-DomainError values default to 500.
//
// Note: §3.2 fixed the CodeInvalidInput mapping from 400 → 422.
// Pre-§3.2 this mapper incorrectly sent invalid input to 400, while
// every inline handler check (e.g., task.HandleTaskCreate) was sending
// the same error to 422. The centralisation in server.BaseHTTPService.
// Wrap + server.mapStatus adopts the 422 semantic universally; this
// helper is kept here as the public API for any non-Wrap caller (CLI
// commands outside src/internal/cli/*, integration tests, custom
// embeds). Both rewires land the same status; callers that use
// httputil.MapError directly get the corrected 422.
func MapError(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	if errors.Is(err, core.ErrInvalidInput) {
		return http.StatusUnprocessableEntity, err.Error()
	}
	if errors.Is(err, core.ErrSchemaConflict) {
		return http.StatusConflict, err.Error()
	}
	var de *core.DomainError
	if errors.As(err, &de) {
		switch de.Code {
		case core.CodeNotFound:
			return http.StatusBadRequest, de.Message
		case core.CodeInvalidInput:
			return http.StatusUnprocessableEntity, de.Message
		case core.CodeSchemaConflict:
			return http.StatusConflict, de.Message
		case core.CodeInvalidSchema:
			return http.StatusUnprocessableEntity, de.Message
		case core.CodeUnauthorized:
			return http.StatusUnauthorized, de.Message
		default:
			return http.StatusInternalServerError, de.Message
		}
	}
	return http.StatusInternalServerError, err.Error()
}

// DecodeJSON reads the JSON body of r, applies the standard MaxBodyBytes
// cap (via http.MaxBytesReader — prevents unlimited RAM consumption on
// hostile payloads), and decodes strictly into a *T.
//
// On success returns the populated dst + nil.
//
// On any decode failure — empty body, malformed JSON, body larger than
// MaxBodyBytes, trailing data after a JSON value, unknown field, type
// mismatch on a typed field — returns a *core.DomainError{Code:
// CodeInvalidInput, Message: <detail>, Field: <offending key>, Err:
// core.ErrInvalidInput}. Returning *core.DomainError is what makes
// §10 integrate cleanly with §3.2 Wrap: callers can `return err`, and
// Wrap's mapStatus maps CodeInvalidInput to HTTP 422 (Unprocessable
// Entity) — fixing the §3.2 silent bug where every inline handler
// check sent invalid input to 422 but the central helpers sent it to
// 400.
//
// The Field carries the JSON pointer to the offending key (when known),
// and DomainError.Error() renders it as "msg (field)" so a single
// WriteError call survives the structured-detail loss that mapStatus
// triggers. Callers that need a structured JSON `field` attribute
// (vs the inline-parentheses rendering) still have the legacy
// WriteErrorWithCode path available for direct use.
//
// DecodeStrict is preserved as the io.Reader-only engine used by CLI
// stdin reading (src/internal/cli/env/env.go:DecodeStdin) and any
// non-HTTP caller that has only a []byte source.
//
// T must be a non-pointer struct (or any non-pointer value that json
// can decode into). Passing *MyStruct will JSON-decode into a nil
// pointer via DecodeStrict and panic at runtime — go-generics does not
// permit a runtime guard without imposing a per-call reflection cost,
// so the constraint is documented here instead.
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var zero T
	// MaxBodyBytes is int64-typed so this assignment matches
	// http.MaxBytesReader's parameter type without a cast.
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	var dst T
	_, field, msg, ok := DecodeStrict(r.Body, &dst)
	if !ok {
		return zero, &core.DomainError{
			Code:    core.CodeInvalidInput,
			Message: msg,
			Field:   field,
			Err:     core.ErrInvalidInput,
		}
	}
	return dst, nil
}

// RespondJSON is the §10 alias for WriteJSON. New handlers prefer this
// name because it pairs semantically with DecodeJSON (request + response
// sides of the same call). It writes the same envelope as WriteJSON
// (Content-Type: application/json, status, JSON-encoded body) and
// delegates to WriteJSON so there is no source-of-truth drift.
//
// Callers adopting §10 should use RespondJSON; legacy callers of
// WriteJSON continue to work unchanged. Both names live in v1.
func RespondJSON(w http.ResponseWriter, status int, data interface{}) {
	WriteJSON(w, status, data)
}
