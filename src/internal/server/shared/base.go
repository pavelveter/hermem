package shared

import (
	"errors"
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// BaseHTTPService is the shared base type for every HTTP shell under
// src/internal/server/*/. It bundles the two cross-cutting transport
// dependencies that every handler reads per request:
//
//   - Metrics — counter/histogram increments; never held by the domain
//     Service. Every shell writes to it on at least one error path
//     (Metrics.IncErr) and several success paths (IncSearch, IncStore,
//     IncEdge, IncTaskExec, …).
//   - Refs    — atomic *serverstate.State holder. Lets SIGHUP-driven
//     state swaps apply to in-flight handlers without reconstructing
//     the shell; only the shells that gate by schema or
//     category/relation validity need it. shells that don't (e.g.,
//     health, contradiction, reembed, timeline) hold Refs=nil and the
//     handler must avoid calling s.Refs.Load() (Wrap doesn't touch
//     Refs, so this is safe when the handler never reads it).
//
// Shells embed BaseHTTPService by value so the field accesses
// `s.Metrics` and `s.Refs` work unchanged in handler code via Go's
// field promotion. The constructor signature stays `New(svc, m, refs)`
// (or its shell-specific extra-arg variant for MemDim, DedupThreshold,
// DefaultPolicy) so cli/serve.go + integration_test.go + every other
// caller needs zero edits.
//
// Twelve shells now share this base: contradiction, edge, graph,
// ingest, memory, migration, reembed, retention, retrieval, task,
// timeline. health keeps its own shape (`Svc` only — no metrics, no
// refs) by design; adding observability there adds noise without
// value (the dashboard already counts health failures via the
// readiness probe count).
type BaseHTTPService struct {
	Metrics *metrics.Metrics
	Refs    *serverstate.Ref
}

// Wrap converts a handler that returns `error` into the
// `http.HandlerFunc` shape the mux requires. Collapses the repeated
//
//	if err != nil {
//	    s.Metrics.IncErr()
//	    httputil.WriteError(w, http.StatusInternalServerError, err.Error())
//	    return
//	}
//
// pattern at every domain-call site into a single `return err`
// statement. The fn is responsible for writing the success response
// itself (httputil.WriteJSON on 200/204 or httputil.WriteError for
// transport-level rejections like 405); Wrap only fires on a non-nil
// return.
//
// On error:
//
//  1. Best-effort Metrics.IncErr (nil-guarded — non-test callers
//     always wire Metrics; in-memory handlers may not).
//  2. Map err → (HTTP status, message) via mapStatus. Behaviour
//     aligns with the explicit inline error→status checks the
//     shells used pre-§3.2:
//       - core.DomainError{Code: CodeNotFound}          → 400
//       - core.DomainError{Code: CodeInvalidInput}      → 422
//       - core.DomainError{Code: CodeSchemaConflict}    → 409
//       - core.DomainError{Code: CodeInvalidSchema}     → 422
//       - core.DomainError{Code: CodeUnauthorized}      → 401
//       - core.ErrInvalidInput  (uncoded)               → 422
//       - core.ErrSchemaConflict (uncoded)              → 409
//       - everything else                                → 500
//  3. Write the standard JSON error envelope (httputil.WriteError).
//
// CodeNotFound is intentionally mapped to 400 (not 404). Pre-§3.2
// every shell that returned a NotFound sentinel treated it as a
// client mistake (wrong id) and reported 400 to preserve the wire
// contract. The 4xx-vs-5xx split is what callers care about, not the
// specific 400-vs-404 distinction.
//
// On a nil error return, fn already wrote the response; Wrap is a
// no-op. This deliberately lets handlers do partial-write patterns
// (e.g., retention.HandleRun writes a GCReport body before RunOnce
// returns an error and Wrap then maps the err to a status — the
// handler stays in charge of body composition).
func (b *BaseHTTPService) Wrap(fn func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			// b is non-nil whenever Wrap is reached (Go's method dispatch
			// would have panicked otherwise) — only the Metrics nil-guard
			// is doing real work. Shells that wire only a domain Service
			// without transport observability may omit Metrics.
			if b.Metrics != nil {
				b.Metrics.IncErr()
			}
			status, msg := mapStatus(err)
			// When err carries a *core.DomainError, route through
			// WriteErrorWithCode so the wire envelope carries the
			// `code` + `field` JSON attributes that pre-§10 inline
			// handlers emitted via the same call. Without this routing
			// path the §10 generic DecodeJSON[T] lossily flattens to
			// WriteError's `{"error":"msg (field)"}` shape, breaking
			// every client that parses the structured attributes.
			// Non-DomainError paths (network errors, context-cancelled,
			// plain fmt.Errorf) keep falling through to WriteError
			// because no structured detail is available.
			var de *core.DomainError
			if errors.As(err, &de) {
				// Use err.Error() (verbatim) so the wire envelope
				// carries the same text pre-§10 inline handlers emitted
				// via WriteErrorWithCode — including fmt.Errorf wrap
				// prefixes like "failed to parse payload:" that operator
				// logs grep on, and DomainError's "msg (field)"
				// inline-parenthesised rendering for the field attr.
				// Pre-§10 inline handlers did the same WriteErrorWithCode
				// call with err.Error() as the msg argument, so option
				// (a) preserves wire bytes byte-for-byte rather than
				// silently dropping wrap-chain context.
				httputil.WriteErrorWithCode(w, status, err.Error(), de.Code, de.Field)
				return
			}
			httputil.WriteError(w, status, msg)
		}
	}
}

// ErrNoServerState is the defensive sentinel returned by shells
// whose handler can fire before *serverstate.State is loaded (e.g.,
// graph.HandleGraphVerify, migration.HandleSchemaFingerprint).
// Production always loads a state; the sentinel exists so the wire
// bytes match the pre-§3.2 inline WriteError(500, "no server state")
// branch. mapStatus falls through to (500, "no server state") for
// any error whose message equals this — keeping the contract
// backwards-compatible without a dedicated mapStatus branch.
var ErrNoServerState = noServerStateError{}

type noServerStateError struct{}

func (noServerStateError) Error() string { return "no server state" }

// mapStatus converts a domain error to its HTTP status + message.
// Pure function — sync shell logic duplicated from the inline
// checks in pre-§3.2 handlers so the Wrap path produces byte-
// identical status codes for the same error return.
//
// CodeNotFound → 400 (matches the explicit
//   `if errors.Is(err, core.ErrNotFound) { httputil.WriteError(w, 400, ...) }`
// mapping in task.HandleTaskStatus / HandleTaskShow). Preserves the
// pre-§3.2 wire contract.
//
// CodeInvalidInput → 422 (NOT 400 — silent bug fix; the pre-§3.2
// httputil.MapError mapped this to 400, contradicting the inline
// task.HandleTaskCreate check that mapped it to 422. centralising
// the wiring into Wrap closes the gap).
//
// CodeSchemaConflict → 409 (Matches httputil.MapError; shells that
//   used shared.RejectSchemaConflict wrote 409 independently).
func mapStatus(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	if errors.Is(err, core.ErrInvalidInput) {
		return http.StatusUnprocessableEntity, err.Error()
	}
	if errors.Is(err, core.ErrSchemaConflict) {
		return http.StatusConflict, err.Error()
	}
	if errors.Is(err, core.ErrNotFound) {
		// Sentinel-only (uncoded DomainError): keep the 400 behaviour
		// the inline checks used pre-§3.2. DomainError-wrapped
		// CodeNotFound falls through to the next branch and gets the
		// 400 mapping there as well — same outcome, two paths.
		return http.StatusBadRequest, err.Error()
	}
	var de *core.DomainError
	if errors.As(err, &de) {
		// Use err.Error() (which renders de.Message + "(de.Field)" when
		// Field is set) instead of de.Message verbatim. Pre-§3.2 inline
		// checks wrote httputil.WriteError(w, status, err.Error()) —
		// the wire bytes carried the field annotation that clients
		// like the validation-error dashboard parse. Preserving that
		// contract here keeps the §3.2 refactor invisible to consumers.
		switch de.Code {
		case core.CodeNotFound:
			return http.StatusBadRequest, err.Error()
		case core.CodeInvalidInput:
			return http.StatusUnprocessableEntity, err.Error()
		case core.CodeSchemaConflict:
			return http.StatusConflict, err.Error()
		case core.CodeInvalidSchema:
			return http.StatusUnprocessableEntity, err.Error()
		case core.CodeUnauthorized:
			return http.StatusUnauthorized, err.Error()
		case core.CodeInternalError:
			return http.StatusInternalServerError, err.Error()
		default:
			return http.StatusInternalServerError, err.Error()
		}
	}
	return http.StatusInternalServerError, err.Error()
}
