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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// WriteErrorWithCode writes a structured JSON error response from err.
// When err is a *core.DomainError (or wraps one via fmt.Errorf %w),
// the code + field JSON attributes are populated; otherwise the
// response falls through to WriteError's bare-envelope shape
// ({"error": err.Error()}). Replaces the pre-§7.2
// (w, status, msg, code, field) 5-arg shape with this 3-arg form.
//
// Wire-byte preservation: when err IS a *core.DomainError, the
// envelope uses err.Error() (NOT de.Error()) as the `error` field
// so wrap-chain prefixes like "failed to parse payload:" survive
// the round-trip — matching the pre-§7.2 base.go path which
// explicitly chose err.Error() for the same reason. DomainError's
// own "msg (field)" inline rendering is preserved when callers
// pass a bare *core.DomainError literal (no wrap).
//
// Callers that already have a *core.DomainError (e.g. §3.2 Wrap,
// §10 DecodeJSON[T]) just pass it through; callers with a plain
// error pass it directly and get the bare-envelope fallback.
// Inline-validation sites that previously hard-coded
// (msg, code, field) tuples construct a DomainError literal at the
// call site — the wire envelope gains the "(field)" suffix on the
// message that DomainError.Error() renders, which is additive
// (clients that parsed code/field separately keep working; human
// readers see the field name inline).
func WriteErrorWithCode(w http.ResponseWriter, status int, err error) {
	var de *core.DomainError
	if errors.As(err, &de) {
		WriteJSON(w, status, core.ErrorResponse{
			Error: err.Error(),
			Code:  de.Code,
			Field: de.Field,
		})
		return
	}
	WriteError(w, status, err.Error())
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

// --- §2 AUDIT CLOSURE: SafeStreamFetch + ErrResponseTooLarge ----------
//
// SafeStreamFetch is the §2 audit closure for outbound HTTP. The audit
// flagged resource leaks via defer-less body reads on outbound HTTP —
// every AI provider client (OllamaEmbedder / OpenAIEmbedder /
// OllamaLLMExtractor / OpenAILLMExtractor / rerankers) wraps into the
// ai package's httpClient.doPOST helper which has its own retry loop
// (ResilientClient) and a defer Body.Close but ZERO body-size cap on
// the success-stage io.ReadAll path. A hostile or buggy downstream
// provider could ship a 4 GB response on a 4xx or a 2xx body and the
// reader would consume the entire stream into RAM.
//
// SafeStreamFetch is the GENERIC Get-with-body-cap helper for any new
// outbound HTTP caller that does not already use ai/httpClient. The
// ai package's existing path is not migrated wholesale because its
// ResilientClient retry semantics are battle-tested against the existing
// client_test.go suite; body-cap is added in doPOST via a separate
// inlined LimitReader guard rather than re-routed through SafeStreamFetch.
//
// Behavior:
//
//   - 2xx → returns body bytes (capped at maxBytes).
//   - 4xx → fails-fast, returns fmt.Errorf("%d: %s", status, body-snippet).
//     The body snippet itself is capped at `safeStreamFetchSnippetBytes`
//     so a hostile 4xx payload cannot amplify RAM through this path.
//   - 5xx / 429 → retries with jittered exponential backoff
//     (safeStreamFetchBackoffs). If all attempts fail, returns the
//     last transient error (HTTP %d (transient)).
//   - Network error → returns the wrapped error; ctx cancellation aborts
//     any in-flight sleep between retries.
//
// maxBytes <= 0 treats the body as discarded and returns []byte{} + nil.
// Useful when a caller only cares about reachability, not payload
// (e.g. an external health probe).
//
// Caller-supplied headers (Authorization, custom User-Agent) cannot be
// attached via the URL-only signature. For requests that need headers
// (OpenAI Bearer token, custom Accept, etc.) wrap net/http directly or
// extend this helper with a req-based variant.
//
// The http.Client used is a fresh pointer with safeStreamFetchMaxBodyTimeout
// (30s). Callers needing per-request timeout control must wrap net/http
// directly.
//
// Body lifetime: `resp.Body` is closed via defer on every terminal
// response (2xx, 4xx). 5xx / 429 paths drain the body via
// io.Copy(io.Discard) BEFORE Close so the underlying TCP connection
// is returned to the keep-alive pool instead of being RST on Close.
// This mirrors ResilientClient.Do's drain-on-transient pattern.
func SafeStreamFetch(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	// safeStreamFetch is the testable inner form (the public
	// SafeStreamFetch call is preserved for the §2 audit signature
	// contract — URL + ctx + maxBytes).
	return safeStreamFetch(ctx, http.DefaultClient, url, maxBytes)
}

// safeStreamFetch is the testable inner helper. `inner` may be nil in
// which case a fresh *http.Client with safeStreamFetchMaxBodyTimeout is
// constructed per call. Production callers go through SafeStreamFetch
// which uses http.DefaultClient; tests may pass `srv.Client()` or a
// fresh client for timeout control.
func safeStreamFetch(ctx context.Context, inner *http.Client, url string, maxBytes int64) ([]byte, error) {
	if inner == nil {
		inner = &http.Client{Timeout: safeStreamFetchMaxBodyTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	var lastErr error
	for i := 0; i < len(safeStreamFetchBackoffs)+1; i++ {
		if i > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !sleepWithCtx(ctx, safeStreamFetchBackoffs[i-1]) {
				return nil, ctx.Err()
			}
		}
		resp, err := inner.Do(req.Clone(ctx))
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			// Drain so the connection returns to the keep-alive pool.
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d (transient)", resp.StatusCode)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		// Terminal response — read + close on every code path.
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, safeStreamFetchSnippetBytes))
			return nil, fmt.Errorf("%d: %s", resp.StatusCode, string(snippet))
		}
		if maxBytes <= 0 {
			_, _ = io.Copy(io.Discard, resp.Body)
			return []byte{}, nil
		}
		// Read up to maxBytes+1 to detect overflow without committing
		// the full read. If we read more than maxBytes, return
		// ErrResponseTooLarge; the partial body is discarded.
		buf, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read body: %w", err)
		}
		if int64(len(buf)) > maxBytes {
			return nil, fmt.Errorf("%w: body=%d bytes (cap=%d)", ErrResponseTooLarge, len(buf), maxBytes)
		}
		return buf, nil
	}
	return nil, lastErr
}

// MaxResponseBodyBytes is the recommended cap for SafeStreamFetch and
// ai/http.go::doPOST body reads. 16 MiB mirrors MaxBodyBytes (1 MiB) × a
// factor: AI payloads (embeddings + LLM JSON envelopes) are typically a
// few KB to a few MB; 16 MiB is generous for legitimate responses while
// still bounding a hostile-or-buggy 64 GB body to a manageable RAM cap.
var MaxResponseBodyBytes int64 = 16 * 1024 * 1024

// ErrResponseTooLarge is returned by SafeStreamFetch (and surfaced via
// fmt.Errorf %w wrapping by ai/http.go::doPOST) when the response body
// exceeds the configured cap. Callers should map this to a 502 Bad
// Gateway to upstream consumers (the body could not be trusted as a
// coherent response from the downstream provider).
var ErrResponseTooLarge = errors.New("httputil: response body exceeds configured cap")

// safeStreamFetchSnippetBytes caps the body snippet included in a 4xx
// error message. 16 KiB is enough for the typical error JSON envelope
// {"error": "..."} while guarding against 1 GiB hostile 4xx bodies.
const safeStreamFetchSnippetBytes int64 = 16 * 1024

// safeStreamFetchMaxBodyTimeout is the timeout applied to the http.Client
// when SafeStreamFetch is called via the URL-only signature. Matches
// ai/httpClient defaults; per-request overrides must use the req-based
// variant or wrap net/http directly.
const safeStreamFetchMaxBodyTimeout = 30 * time.Second

// safeStreamFetchBackoffs is the retry ladder applied on 5xx / 429
// responses. Matches ai/client.go::defaultBackoffs but lives in this
// package because SafeStreamFetch is independent of ResilientClient.
// 200ms / 500ms / 1s / 2s gives 4 total attempts (1 initial + 3 retries).
var safeStreamFetchBackoffs = []time.Duration{
	200 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
}

// sleepWithCtx blocks for d plus a small jitter up to d/4, returning
// false immediately if ctx is cancelled. Mirrors ai/client.go::backoffSleep
// (kept independent so httputil has zero ai-package imports).
func sleepWithCtx(ctx context.Context, d time.Duration) bool {
	jitter := time.Duration(rand.Int63n(int64(d)/4 + 1))
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d + jitter):
		return true
	}
}
