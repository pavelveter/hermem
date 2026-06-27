package httputil

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// TestDecodeStrict_Valid covers the happy path — well-formed JSON decodes
// into the destination and ok=true with empty code/field/msg.
func TestDecodeStrict_Valid(t *testing.T) {
	var dst struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	code, field, msg, ok := DecodeStrict(strings.NewReader(`{"id":"a","name":"b"}`), &dst)
	if !ok || code != "" || field != "" || msg != "" {
		t.Fatalf("valid decode: want ok=true empty tuple, got code=%q field=%q msg=%q", code, field, msg)
	}
	if dst.ID != "a" || dst.Name != "b" {
		t.Fatalf("payload: want {a,b}, got %+v", dst)
	}
}

// TestDecodeStrict_EmptyBody — completely empty input must surface
// (empty_body, "", "request body is empty", false), NOT a confusing
// syntax error.
func TestDecodeStrict_EmptyBody(t *testing.T) {
	code, _, _, ok := DecodeStrict(strings.NewReader(""), &struct{}{})
	if ok || code != "empty_body" {
		t.Fatalf("empty body: want code=empty_body ok=false, got code=%q ok=%v", code, ok)
	}
}

// TestDecodeStrict_UnknownField guarantees strict mode rejects typos.
// Returns code=unknown_field + the offending field name so a handler can
// surface a 422 with the field-level diagnostic.
func TestDecodeStrict_UnknownField(t *testing.T) {
	var dst struct {
		ID string `json:"id"`
	}
	code, field, msg, ok := DecodeStrict(strings.NewReader(`{"id":"a","extra":"oops"}`), &dst)
	if ok || code != "unknown_field" || field != "extra" || !strings.Contains(msg, "extra") {
		t.Fatalf("unknown field: want code=unknown_field field=extra msg-containing-extra, got code=%q field=%q msg=%q ok=%v", code, field, msg, ok)
	}
}

// TestDecodeStrict_TrailingData — a JSON value followed by a second value
// must be rejected. Without this a caller could smuggle a payload past
// strict decode.
func TestDecodeStrict_TrailingData(t *testing.T) {
	var dst struct {
		ID string `json:"id"`
	}
	code, _, _, ok := DecodeStrict(strings.NewReader(`{"id":"a"}{"id":"b"}`), &dst)
	if ok || code != "trailing_data" {
		t.Fatalf("trailing data: want code=trailing_data ok=false, got code=%q ok=%v", code, ok)
	}
}

// TestDecodeStrict_TypeError — wrong JSON type for a typed field surfaces
// (invalid_type, <field>, ...) so handlers can render 422 with the field
// rather than a generic 400.
func TestDecodeStrict_TypeError(t *testing.T) {
	var dst struct {
		N int `json:"n"`
	}
	code, field, _, ok := DecodeStrict(strings.NewReader(`{"n":"not_a_number"}`), &dst)
	if ok || code != "invalid_type" || field != "n" {
		t.Fatalf("type error: want code=invalid_type field=n, got code=%q field=%q ok=%v", code, field, ok)
	}
}

// TestDecodeStrict_Malformed covers the catch-all: any garbage that is
// neither EOF nor a typed unmarshal error must produce code=bad_json.
func TestDecodeStrict_Malformed(t *testing.T) {
	var dst struct {
		ID string `json:"id"`
	}
	code, _, _, ok := DecodeStrict(strings.NewReader(`{garbage}`), &dst)
	if ok || code != "bad_json" {
		t.Fatalf("malformed: want code=bad_json, got code=%q ok=%v", code, ok)
	}
}

// TestDecodeStrict_DisallowsUnknownAtInnerLevel — nested unknown fields
// are also rejected. Belt-and-braces against any future relaxation of
// DisallowUnknownFields.
func TestDecodeStrict_DisallowsUnknownAtInnerLevel(t *testing.T) {
	var dst struct {
		Inner struct {
			OK string `json:"ok"`
		} `json:"inner"`
	}
	code, _, _, ok := DecodeStrict(strings.NewReader(`{"inner":{"ok":"x","extra":"y"}}`), &dst)
	if ok || code != "unknown_field" {
		t.Fatalf("nested unknown: want code=unknown_field, got code=%q ok=%v", code, ok)
	}
}

// TestWriteJSON_HappyPath encodes a struct and writes status+content-type.
// Used by WriteError / WriteErrorWithCode; if this regressed every handler
// output would break.
func TestWriteJSON_HappyPath(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteJSON(rr, http.StatusOK, map[string]string{"hello": "world"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type: want application/json, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), `"hello":"world"`) {
		t.Fatalf("body: want JSON containing hello=world, got %q", rr.Body.String())
	}
}

// TestWriteError_UsesErrorResponse shape — the {"error": "..."} envelope
// is the contract every consumer parses; a regression here is silent.
func TestWriteError_UsesErrorResponse(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteError(rr, http.StatusBadRequest, "boom")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"error":"boom"`) {
		t.Fatalf("body: want error=boom envelope, got %q", rr.Body.String())
	}
}

// TestWriteErrorWithCode_IncludesField lets handlers surface a 422 with
// both a top-level message and per-field context — the field is what
// downstream UIs use to highlight the offending input.
func TestWriteErrorWithCode_IncludesField(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteErrorWithCode(rr, http.StatusUnprocessableEntity, &core.DomainError{
		Code: "required_field_missing", Message: "missing", Field: "email",
	})
	body := rr.Body.String()
	for _, want := range []string{`"error":"missing (email)"`, `"code":"required_field_missing"`, `"field":"email"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %q", want, body)
		}
	}
}

// TestParseIntParam_ReturnsDefault covers the missing-param branch.
func TestParseIntParam_ReturnsDefault(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", nil)
	if got := ParseIntParam(r, "n", 7); got != 7 {
		t.Fatalf("missing param: want default 7, got %d", got)
	}
}

// TestParseIntParam_ParsesValid — well-formed int passes through.
func TestParseIntParam_ParsesValid(t *testing.T) {
	u, _ := url.Parse("/x?n=42")
	r := httptest.NewRequest("GET", u.String(), nil)
	if got := ParseIntParam(r, "n", 7); got != 42 {
		t.Fatalf("valid: want 42, got %d", got)
	}
}

// TestParseIntParam_FallsBackOnGarbage — unparseable input must default,
// not silently propagate 0 (which would look like a valid answer).
func TestParseIntParam_FallsBackOnGarbage(t *testing.T) {
	u, _ := url.Parse("/x?n=banana")
	r := httptest.NewRequest("GET", u.String(), nil)
	if got := ParseIntParam(r, "n", 7); got != 7 {
		t.Fatalf("garbage: want default 7, got %d", got)
	}
}

// ---------------------------------------------------------------------
// DecodeJSON[T] + RespondJSON (§10 helpers)
// ---------------------------------------------------------------------

type decodeJSONTestPayload struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TestDecodeJSON_HappyPath verifies the §10 DecodeJSON[T] generic helper
// successfully decodes a JSON body into a typed payload, surface the
// zero value of T on error, and apply MaxBytesReader transparently.
func TestDecodeJSON_HappyPath(t *testing.T) {
	r := httptest.NewRequest("POST", "/x", strings.NewReader(`{"id":"abc","name":"widget"}`))
	got, err := DecodeJSON[decodeJSONTestPayload](httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("happy decode: want nil err, got %v", err)
	}
	if got.ID != "abc" || got.Name != "widget" {
		t.Fatalf("payload: want {abc, widget}, got %+v", got)
	}
}

// TestDecodeJSON_EmptyBodyReturnsDomainError verifies the §10 contract:
// empty body surfaces as a *core.DomainError{Code: CodeInvalidInput,
// Message: containing "empty"}. The error must NOT be a bare string.
// §3.2 Wrap's mapStatus will recognise CodeInvalidInput and map to 422
// instead of the 500 a plain error would produce.
func TestDecodeJSON_EmptyBodyReturnsDomainError(t *testing.T) {
	r := httptest.NewRequest("POST", "/x", strings.NewReader(""))
	_, err := DecodeJSON[decodeJSONTestPayload](httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("empty body: want DomainError, got nil")
	}
	var de *core.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("empty body: want *core.DomainError, got %T: %v", err, err)
	}
	if de.Code != core.CodeInvalidInput {
		t.Fatalf("empty body code: want CodeInvalidInput, got %q", de.Code)
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty body message: want containing \"empty\", got %q", err.Error())
	}
}

// TestDecodeJSON_MalformedReturnsDomainError — any non-empty/non-trailing
// JSON garbage must surface as CodeInvalidInput (not the default 500).
func TestDecodeJSON_MalformedReturnsDomainError(t *testing.T) {
	r := httptest.NewRequest("POST", "/x", strings.NewReader("{garbage}"))
	_, err := DecodeJSON[decodeJSONTestPayload](httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("malformed: want DomainError, got nil")
	}
	var de *core.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("malformed: want *core.DomainError, got %T: %v", err, err)
	}
	if de.Code != core.CodeInvalidInput {
		t.Fatalf("malformed code: want CodeInvalidInput, got %q", de.Code)
	}
}

// TestDecodeJSON_UnknownFieldPopulatesFieldAnnotation verifies the §10
// contract: an unknown field surfaces as CodeInvalidInput AND the
// DomainError.Field carries the offending key. Wrap → mapStatus renders
// this as "msg (field)" inline so the wire byte still tells the client
// WHICH field was wrong. Without the field propagation the user could
// not tell which input to fix.
func TestDecodeJSON_UnknownFieldPopulatesFieldAnnotation(t *testing.T) {
	r := httptest.NewRequest("POST", "/x", strings.NewReader(`{"id":"a","extra":"oops"}`))
	_, err := DecodeJSON[decodeJSONTestPayload](httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("unknown field: want DomainError, got nil")
	}
	var de *core.DomainError
	if !errors.As(err, &de) || de.Code != core.CodeInvalidInput {
		t.Fatalf("unknown field: want CodeInvalidInput DomainError, got %T: %v", err, err)
	}
	if de.Field != "extra" {
		t.Fatalf("unknown field DomainError.Field: want \"extra\", got %q", de.Field)
	}
	if !strings.Contains(err.Error(), "extra") {
		t.Fatalf("err.Error() should embed field name; got %q", err.Error())
	}
}

// TestDecodeJSON_TypeErrorPopulatesFieldAnnotation — a JSON type mismatch
// on a typed field must surface as CodeInvalidInput with DomainError.Field
// set to the offending JSON pointer. This is what makes a real client
// able to pinpoint WHICH input is wrong.
func TestDecodeJSON_TypeErrorPopulatesFieldAnnotation(t *testing.T) {
	r := httptest.NewRequest("POST", "/x", strings.NewReader(`{"id":"abc","name":42}`))
	_, err := DecodeJSON[decodeJSONTestPayload](httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("type error: want DomainError, got nil")
	}
	var de *core.DomainError
	if !errors.As(err, &de) || de.Code != core.CodeInvalidInput {
		t.Fatalf("type error: want CodeInvalidInput DomainError, got %T: %v", err, err)
	}
	if de.Field != "name" {
		t.Fatalf("type error Field: want \"name\", got %q", de.Field)
	}
}

// TestDecodeJSON_MaxBytesOverflowReturnsError verifies that a body larger
// than the configured cap surfaces as an error from DecodeJSON rather than
// silently consuming unbounded RAM. We don't care about the concrete
// error type here — http.MaxBytesReader returns a *http.MaxBytesError —
// only that the decode aborts before allocating the full payload.
//
// MaxBodyBytes is a var (not const) so this test can shrink the cap
// locally and don't have to allocate a real 1 MiB body. t.Cleanup
// restores the package value after the test finishes to avoid
// contaminating other tests that rely on the default 1 MiB cap.
func TestDecodeJSON_MaxBytesOverflowReturnsError(t *testing.T) {
	orig := MaxBodyBytes
	MaxBodyBytes = 64
	t.Cleanup(func() { MaxBodyBytes = orig })
	huge := strings.Repeat("a", 65)
	r := httptest.NewRequest("POST", "/x", strings.NewReader(huge))
	_, err := DecodeJSON[decodeJSONTestPayload](httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("overflow body: want error, got nil")
	}
}

// TestDecodeJSON_TrailingDataReturnsDomainError — two JSON values
// concatenated (without separator, e.g. `{"a":"b"}{"a":"c"}`) must
// surface as CodeInvalidInput. Without this guard, hostile callers
// could smuggle a second payload past strict decode.
func TestDecodeJSON_TrailingDataReturnsDomainError(t *testing.T) {
	r := httptest.NewRequest("POST", "/x", strings.NewReader(`{"id":"a"}{"id":"b"}`))
	_, err := DecodeJSON[decodeJSONTestPayload](httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("trailing data: want DomainError, got nil")
	}
	var de *core.DomainError
	if !errors.As(err, &de) || de.Code != core.CodeInvalidInput {
		t.Fatalf("trailing data: want CodeInvalidInput DomainError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing data message: want containing \"trailing\", got %q", err.Error())
	}
}

// TestDecodeJSON_NestedUnknownFieldReturnsDomainError — an unknown
// field nested inside a struct field surfaces as CodeInvalidInput.
// Belt-and-braces against any future relaxation of DisallowUnknownFields
// on inner structs (since the constraint is enforced per-Decoder path).
func TestDecodeJSON_NestedUnknownFieldReturnsDomainError(t *testing.T) {
	type outer struct {
		Inner struct {
			OK string `json:"ok"`
		} `json:"inner"`
	}
	r := httptest.NewRequest("POST", "/x", strings.NewReader(`{"inner":{"ok":"x","extra":"y"}}`))
	_, err := DecodeJSON[outer](httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("nested unknown: want DomainError, got nil")
	}
	var de *core.DomainError
	if !errors.As(err, &de) || de.Code != core.CodeInvalidInput {
		t.Fatalf("nested unknown: want CodeInvalidInput DomainError, got %T: %v", err, err)
	}
	if de.Field != "extra" {
		t.Fatalf("nested unknown Field: want \"extra\", got %q", de.Field)
	}
}

// TestRespondJSON_DelegatesToWriteJSON pins the RespondJSON contract: same
// header + status + body behaviour as WriteJSON. If WriteJSON is updated
// (e.g., a future Content-Type negotiation), RespondJSON inherits the
// change for free since it delegates.
func TestRespondJSON_DelegatesToWriteJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	RespondJSON(rr, http.StatusOK, map[string]string{"hello": "world"})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type: want application/json, got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), `"hello":"world"`) {
		t.Fatalf("body: want JSON containing hello=world, got %q", rr.Body.String())
	}
}

// ---------------------------------------------------------------------
// §2 audit closure: SafeStreamFetch
// ---------------------------------------------------------------------

// TestSafeStreamFetch_HappyPath pins the success path: 2xx body is
// returned verbatim (capped at maxBytes). The response body is the
// canonical "hello world" JSON envelope. This is the path every new
// outbound GET caller (a future admin RPC, an external health probe,
// etc.) goes through on the happy case.
func TestSafeStreamFetch_HappyPath(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method: want GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	got, err := SafeStreamFetch(t.Context(), srv.URL, 1<<20)
	if err != nil {
		t.Fatalf("happy: want nil err, got %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body: want %q, got %q", body, got)
	}
}

// TestSafeStreamFetch_4xxFailFast pins the 4xx path: 4xx surfaces as
// fmt.Errorf("%d: %s", status, body-snippet) in a single attempt
// (NO retry — 4xx is terminal). The body snippet is included so callers
// can surface the provider's error JSON to the user.
func TestSafeStreamFetch_4xxFailFast(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad model"}`))
	}))
	defer srv.Close()

	_, err := SafeStreamFetch(t.Context(), srv.URL, 1<<20)
	if err == nil {
		t.Fatal("4xx: want error, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("4xx error: want containing \"400\", got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "bad model") {
		t.Fatalf("4xx error: want snippet containing \"bad model\", got %q", err.Error())
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("4xx attempts: want 1 (no retry), got %d", got)
	}
}

// TestSafeStreamFetch_5xxRetriesThenSucceeds pins the retry-then-success
// path: a 5xx on attempt #1 triggers retry with jittered backoff. After
// 3 attempts total, the 4th attempt returns 2xx + body, which the
// helper returns. The test uses a 1ms backoff ladder so it runs in
// milliseconds (the production ladder is 200ms / 500ms / 1s / 2s).
func TestSafeStreamFetch_5xxRetriesThenSucceeds(t *testing.T) {
	origBackoffs := safeStreamFetchBackoffs
	safeStreamFetchBackoffs = []time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond}
	t.Cleanup(func() { safeStreamFetchBackoffs = origBackoffs })

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	got, err := SafeStreamFetch(t.Context(), srv.URL, 1<<20)
	if err != nil {
		t.Fatalf("retry-then-success: want nil err, got %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("body: want %q, got %q", "ok", got)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("calls: want 3 (1 initial + 2 retries before success), got %d", got)
	}
}

// TestSafeStreamFetch_5xxAttemptsExhausted pins the failure path when
// every retry returns 5xx: after 4 attempts total (1 + 3 backoffs), the
// helper returns the last transient error "HTTP 502 (transient)". A
// caller should map this to a 502 Bad Gateway upstream.
func TestSafeStreamFetch_5xxAttemptsExhausted(t *testing.T) {
	origBackoffs := safeStreamFetchBackoffs
	safeStreamFetchBackoffs = []time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond}
	t.Cleanup(func() { safeStreamFetchBackoffs = origBackoffs })

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := SafeStreamFetch(t.Context(), srv.URL, 1<<20)
	if err == nil {
		t.Fatal("5xx exhausted: want error, got nil")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("exhausted error: want containing \"502\", got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "transient") {
		t.Fatalf("exhausted error: want containing \"transient\" (retryable tag), got %q", err.Error())
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("calls: want 4 (1 initial + 3 retries = len(safeStreamFetchBackoffs)+1), got %d", got)
	}
}

// TestSafeStreamFetch_TooLargeReturnsErrResponseTooLarge pins the
// body-cap path: when the response body exceeds maxBytes, the helper
// reads maxBytes+1 bytes to detect overflow, then returns
// ErrResponseTooLarge (wrapped with %w). The partial body is
// discarded so a hostile 1 GB payload cannot amplify RAM.
func TestSafeStreamFetch_TooLargeReturnsErrResponseTooLarge(t *testing.T) {
	// Use a custom *http.Client with no timeout so the test cannot be
	// gated by safeStreamFetchMaxBodyTimeout. 1 MiB body is below
	// MaxResponseBodyBytes so without the helper's own cap the test
	// would succeed.
	client := &http.Client{Timeout: 30 * time.Second}
	huge := bytes.Repeat([]byte("a"), 1<<20) // 1 MiB body, 1 KiB cap
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(huge)
	}))
	defer srv.Close()

	_, err := safeStreamFetch(t.Context(), client, srv.URL, 1024) // 1 KiB cap
	if err == nil {
		t.Fatal("cap exceeded: want error, got nil")
	}
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("cap exceeded: want errors.Is(err, ErrResponseTooLarge)=true, got %v", err)
	}
	// The wrapped error also reports the actual size and the cap — useful
	// for logs / metrics when an upstream starts streaming a larger body
	// than expected.
	if !strings.Contains(err.Error(), "cap=1024") {
		t.Fatalf("cap exceeded: want error containing \"cap=1024\", got %q", err.Error())
	}
}

// TestSafeStreamFetch_SnippetCappedAt16KiB pins the 4xx body-snippet
// cap: even a hostile 1 MB 4xx body produces an error whose snippet
// is itself capped at safeStreamFetchSnippetBytes (16 KiB). Without
// this guard, a malicious provider could ship a 100 MB 4xx body that
// the helper would dutifully read into RAM for the error message.
func TestSafeStreamFetch_SnippetCappedAt16KiB(t *testing.T) {
	huge4xx := bytes.Repeat([]byte("X"), 1<<20) // 1 MiB body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write(huge4xx)
	}))
	defer srv.Close()

	_, err := SafeStreamFetch(t.Context(), srv.URL, 1<<20)
	if err == nil {
		t.Fatal("oversized 4xx body: want error, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("4xx snippet: want containing \"403\", got %q", err.Error())
	}
	// The included snippet is bounded — substring count of "X" should
	// be exactly safeStreamFetchSnippetBytes (16 KiB), not the full 1 MiB.
	snippetLen := strings.Count(err.Error(), "X")
	if snippetLen != int(safeStreamFetchSnippetBytes) {
		t.Fatalf("4xx snippet length: want %d (cap), got %d (helper amplified the malicious body)",
			safeStreamFetchSnippetBytes, snippetLen)
	}
}

// TestSafeStreamFetch_MaxBytesLEZeroDiscardsBody pins the "probe-only"
// path: when the caller passes maxBytes <= 0, the body is read-and-
// discarded; an empty []byte{} + nil is returned. Useful for callers
// implementing a reachability check without needing the payload.
func TestSafeStreamFetch_MaxBytesLEZeroDiscardsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("this body should NEVER reach the caller"))
	}))
	defer srv.Close()

	got, err := SafeStreamFetch(t.Context(), srv.URL, 0)
	if err != nil {
		t.Fatalf("probe-only: want nil err, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("probe-only: want empty []byte, got %q", got)
	}
}

// TestSafeStreamFetch_CtxCancelPreAttempt pins the ctx-cancellation
// path: a pre-cancelled ctx surfaces ctx.Err() on the very first
// attempt rather than burning an HTTP round-trip.
func TestSafeStreamFetch_CtxCancelPreAttempt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("would have succeeded"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := SafeStreamFetch(ctx, srv.URL, 1<<20)
	if err == nil {
		t.Fatal("pre-cancelled: want error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled: want errors.Is(ctx.Canceled), got %T: %v", err, err)
	}
}

// TestSafeStreamFetch_CtxCancelMidRetry pins the ctx-cancellation
// path during the inter-attempt backoff: when retry sleeps are in
// progress and ctx fires, the helper exits with ctx.Err() rather
// than completing all attempts.
func TestSafeStreamFetch_CtxCancelMidRetry(t *testing.T) {
	origBackoffs := safeStreamFetchBackoffs
	safeStreamFetchBackoffs = []time.Duration{500 * time.Millisecond, 500 * time.Millisecond, 500 * time.Millisecond}
	t.Cleanup(func() { safeStreamFetchBackoffs = origBackoffs })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		// Cancel once the first retry-backoff starts. Timing
		// slack of ~75ms lets the first attempt + its drain complete.
		time.Sleep(75 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := SafeStreamFetch(ctx, srv.URL, 1<<20)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("mid-retry cancel: want error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-retry cancel: want ctx.Canceled, got %T: %v", err, err)
	}
	// Elapsed should be well under the full 1.5s ladder since the
	// first attempt's backoff is interrupted by cancel.
	if elapsed > 1*time.Second {
		t.Fatalf("mid-retry cancel: took %v — exit path is not honouring ctx (full ladder is 1.5s)", elapsed)
	}
}

// TestErrResponseTooLargeExported pins the exported sentinel error:
// callers reference it via errors.Is so a custom client wrapping
// SafeStreamFetch's output can branch on it ("the body exceeded our
// cap, log + 502 to upstream"). Renaming the var must break this
// test loudly.
func TestErrResponseTooLargeExported(t *testing.T) {
	if ErrResponseTooLarge == nil {
		t.Fatal("ErrResponseTooLarge must be a non-nil sentinel so callers can errors.Is against it")
	}
	if ErrResponseTooLarge.Error() == "" {
		t.Fatal("ErrResponseTooLarge.Error() must be non-empty")
	}
}
