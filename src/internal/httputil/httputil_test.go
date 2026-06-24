package httputil

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
	WriteErrorWithCode(rr, http.StatusUnprocessableEntity, "missing", "required_field_missing", "email")
	body := rr.Body.String()
	for _, want := range []string{`"error":"missing"`, `"code":"required_field_missing"`, `"field":"email"`} {
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
