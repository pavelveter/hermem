package httputil

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
