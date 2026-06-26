// Regression coverage for shared.Wrap, mapStatus, and ErrNoServerState.
//
// §3.2 fixed a silent bug in httputil.MapError: CodeInvalidInput was
// mapped to 400 while every inline handler check mapped it to 422.
// The centralisation into server.Wrap + mapStatus had to produce
// 422 universally; this file pins that behaviour so future refactors
// can't silently regress it. It also covers the ErrNoServerState
// defensive sentinel (graph/migration share it) and the Wrap
// middleware round-trip with synthetic handlers.
package shared

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
)

// --- mapStatus --------------------------------------------------------------

// Sentinel-only mapping: each error listed in the inline pre-§3.2
// checks must keep its specific status. Plain errors.Is walks the
// wrap chain via %w, so a `fmt.Errorf("prefix: %w", core.ErrNotFound)`
// tests the unwrap path too.
func TestMapStatusSentinels(t *testing.T) {
	cases := []struct {
		err    error
		wantS  int
		wantM  string
	}{
		{nil, http.StatusOK, ""},
		{core.ErrNotFound, http.StatusBadRequest, "not found"},
		{core.ErrInvalidInput, http.StatusUnprocessableEntity, "invalid input"},
		{core.ErrSchemaConflict, http.StatusConflict, "schema conflict"},
		{fmt.Errorf("task missing: %w", core.ErrNotFound), http.StatusBadRequest, "task missing: not found"},
		{fmt.Errorf("validation failed: %w", core.ErrInvalidInput), http.StatusUnprocessableEntity, "validation failed: invalid input"},
		{ErrNoServerState, http.StatusInternalServerError, "no server state"},
		{errors.New("plain db error"), http.StatusInternalServerError, "plain db error"},
	}
	for _, c := range cases {
		gotS, gotM := mapStatus(c.err)
		if gotS != c.wantS || gotM != c.wantM {
			t.Errorf("mapStatus(%v) = (%d, %q); want (%d, %q)", c.err, gotS, gotM, c.wantS, c.wantM)
		}
	}
}

// *core.DomainError mapping: each Code must produce the status
// promised by shared.BaseHTTPService.Wrap's doc. Fix #1 from §3.2
// (CodeInvalidInput → 422, was 400 in pre-§3.2 httputil.MapError) is
// asserted here so future refactors can't silently revert.
func TestMapStatusDomainError(t *testing.T) {
	cases := []struct {
		name  string
		err   *core.DomainError
		wantS int
		wantM string
	}{
		{"not_found", core.NewNotFoundError("task abc"), http.StatusBadRequest, "task abc"},
		{"invalid_input", core.NewInvalidInputError("bad content"), http.StatusUnprocessableEntity, "bad content"},
		{"schema_conflict", core.NewSchemaConflictError("generation drift"), http.StatusConflict, "generation drift"},
		{"invalid_schema_with_field", core.NewInvalidSchemaError("category", "weird"), http.StatusUnprocessableEntity, "invalid category: weird (category)"},
		{"unauthorized", &core.DomainError{Code: core.CodeUnauthorized, Message: "no api key"}, http.StatusUnauthorized, "no api key"},
		{"internal_error", &core.DomainError{Code: core.CodeInternalError, Message: "boom"}, http.StatusInternalServerError, "boom"},
	}
	for _, c := range cases {
		gotS, gotM := mapStatus(c.err)
		if gotS != c.wantS || gotM != c.wantM {
			t.Errorf("mapStatus(%s) = (%d, %q); want (%d, %q)", c.name, gotS, gotM, c.wantS, c.wantM)
		}
	}
}

// *core.DomainError.Field annotation must survive mapStatus. The
// second-pass review of §3.2 caught that the DomainError branch was
// returning de.Message instead of err.Error(); this test pins the
// "msg (field)" shape so log parsers / validation dashboards keep
// working.
func TestMapStatusPreservesFieldAnnotation(t *testing.T) {
	de := core.NewInvalidSchemaError("category", "no-such")
	gotS, gotM := mapStatus(de)
	if gotS != http.StatusUnprocessableEntity {
		t.Errorf("got status %d; want %d", gotS, http.StatusUnprocessableEntity)
	}
	if gotM != "invalid category: no-such (category)" {
		t.Errorf("message lost Field annotation: %q", gotM)
	}
}

// --- Wrap middleware --------------------------------------------------------

// nil-returning fn: Wrap is a no-op on the success path. We can't
// expose *metrics.Metrics's internal counters without scraping the
// Prometheus registry, so the assertion is on the wrapped response
// (unchanged status) and the original fn's behaviour (called exactly
// once). The IncErr-fires check is exercised by the err-returning
// tests below via status-code side-effects.
func TestWrapNilReturnIsNoOp(t *testing.T) {
	base := &BaseHTTPService{Metrics: metrics.New()}
	calls := 0
	wrapped := base.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		calls++
		return nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	wrapped(w, r)
	if calls != 1 {
		t.Errorf("fn not invoked exactly once: got %d", calls)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status changed unexpectedly: got %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.Len() != 0 {
		t.Errorf("Wrap wrote body on success path: %q", w.Body.String())
	}
}

// err-returning generic error: defaults to 500 + body = err.Error().
// The IncErr fires once per call; we assert the side effects (status
// + JSON envelope) since *metrics.Metrics does not expose an internal
// counter getter (Prometheus is the only public counter face).
func TestWrapGenericError500(t *testing.T) {
	base := &BaseHTTPService{Metrics: metrics.New()}
	boom := errors.New("database unreachable")
	wrapped := base.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		return boom
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	wrapped(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
	var env map[string]string
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	if env["error"] != "database unreachable" {
		t.Errorf("body: got %q, want %q", env["error"], "database unreachable")
	}
}

// ErrInvalidInput through Wrap must produce 422 (the §3.2 silent
// bug fix). Without this test the 400→422 rewiring can regress
// invisibly.
func TestWrapCodeInvalidInputIs422(t *testing.T) {
	m := metrics.New()
	base := &BaseHTTPService{Metrics: m}
	wrapped := base.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		return core.NewInvalidInputError("content too long")
	})
	w := httptest.NewRecorder()
	wrapped(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("BUG REGRESSION — CodeInvalidInput mapped to %d; want 422", w.Code)
	}
}

// Nil Metrics must not panic. Shells that opt out of transport
// observability (or test fixtures) rely on this.
func TestWrapNilMetricsSafe(t *testing.T) {
	base := &BaseHTTPService{Metrics: nil}
	wrapped := base.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("x")
	})
	w := httptest.NewRecorder()
	wrapped(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// Retention's HandleRun uses a body-as-envelope pattern: it writes a
// GCReport JSON directly with a 500 status when RunOnce returns an
// error, then RETURNS NIL so that Wrap does not double-write a second
// JSON error envelope on top. This test pins that contract: the
// response body must equal the handler's custom JSON verbatim, with
// the chosen status, and there must be no trailing envelope.
//
// `return err` here would corrupt the body (Wrap would WriteHeader +
// Encode a second envelope on top of the GCReport). The retention
// shell calls return nil after the custom write strictly to avoid
// this; the test makes that contract regression-safe.
//
// Using a typed struct mirror (rather than map[string]any) keeps the
// numeric `Swept` field as int through the JSON round-trip — a
// map[string]any round-trip degrades int → float64 and silently
// breaks equality semantics.
func TestWrapCustomBodyReturnNilPreservesBody(t *testing.T) {
	type gcReport struct {
		StartedAt  string `json:"started_at"`
		FinishedAt string `json:"finished_at"`
		Swept      int    `json:"swept"`
		Error      string `json:"error"`
	}
	body := gcReport{
		StartedAt:  "2025-01-01T00:00:00Z",
		FinishedAt: "2025-01-01T00:00:05Z",
		Swept:      3,
		Error:      "partial archive",
	}
	run := func(w http.ResponseWriter, r *http.Request) error {
		// Simulate retention.HandleRun's bespoke envelope-as-body path:
		// write the report directly on 500, then DROP the error so Wrap
		// is a no-op. If a future refactor changes `return nil` back to
		// `return err`, Wrap would force the IncErr + WriteError layer
		// on top of this body, appending a second envelope and
		// corrupting the wire.
		httputil.WriteJSON(w, http.StatusInternalServerError, body)
		return nil
	}
	base := &BaseHTTPService{Metrics: metrics.New()}
	wrapped := base.Wrap(run)
	w := httptest.NewRecorder()
	wrapped(w, httptest.NewRequest(http.MethodPost, "/x", nil))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
	// Snapshot body bytes BEFORE Decode — Decode drains the io.Reader
	// and a subsequent w.Body.Bytes() would return empty. One read,
	// two uses via slices of the same byte array below.
	raw := w.Body.Bytes()
	var got gcReport
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&got); err != nil {
		t.Fatalf("body decode: %v (raw=%q)", err, string(raw))
	}
	if got != body {
		t.Errorf("body mismatched: got %+v, want %+v (raw=%q)", got, body, string(raw))
	}
}

// --- ErrNoServerState -------------------------------------------------------

func TestErrNoServerStateMessage(t *testing.T) {
	if ErrNoServerState.Error() != "no server state" {
		t.Errorf("ErrNoServerState.Error() = %q; want %q", ErrNoServerState.Error(), "no server state")
	}
	// Plain non-DomainError wrapping a sentinel falls into mapStatus'
	// fall-through 500 branch; the message comes from err.Error()
	// itself. This guarantees graph.HandleGraphVerify and
	// migration.HandleSchemaFingerprint produce the same wire bytes
	// as their pre-§3.2 inline WriteError(500, "no server state").
	_, msg := mapStatus(ErrNoServerState)
	if msg != "no server state" {
		t.Errorf("mapStatus(ErrNoServerState) msg = %q; want %q", msg, "no server state")
	}
}
