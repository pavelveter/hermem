package hermem

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := New("http://localhost:8420")
	if c.baseURL != "http://localhost:8420" {
		t.Fatalf("expected baseURL http://localhost:8420, got %s", c.baseURL)
	}
	if c.Memory == nil {
		t.Fatal("Memory client is nil")
	}
	if c.Task == nil {
		t.Fatal("Task client is nil")
	}
	if c.Graph == nil {
		t.Fatal("Graph client is nil")
	}
	if c.Admin == nil {
		t.Fatal("Admin client is nil")
	}
}

func TestNewClientWithAPIKey(t *testing.T) {
	c := New("http://localhost:8420", WithAPIKey("test-key"))
	if c.apiKey != "test-key" {
		t.Fatalf("expected apiKey test-key, got %s", c.apiKey)
	}
}

func TestNewClientWithTimeout(t *testing.T) {
	c := New("http://localhost:8420", WithTimeout(5000))
	if c.httpClient.Timeout != 5000 {
		t.Fatalf("expected timeout 5000, got %v", c.httpClient.Timeout)
	}
}

func TestAPIError(t *testing.T) {
	e := &APIError{
		StatusCode: 404,
		Message:    "not found",
		Code:       "not_found",
	}
	if e.Error() != "hermem: not found (code=not_found, status=404)" {
		t.Fatalf("unexpected error string: %s", e.Error())
	}
}

func TestAPIErrorNoCode(t *testing.T) {
	e := &APIError{
		StatusCode: 500,
		Message:    "internal error",
	}
	if e.Error() != "hermem: internal error (status=500)" {
		t.Fatalf("unexpected error string: %s", e.Error())
	}
}

func TestVersionMismatchSameMajor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Hermem-API-Version", "0.5.0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var called atomic.Bool
	c := New(srv.URL)
	c.OnVersionMismatch = func(server, sdk string) {
		called.Store(true)
	}

	var result interface{}
	_ = c.do(context.Background(), http.MethodGet, "/health", nil, &result)

	if called.Load() {
		t.Fatal("OnVersionMismatch should not be called for same MAJOR")
	}
}

func TestVersionMismatchDifferentMajor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Hermem-API-Version", "1.0.0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var gotServer, gotSDK string
	c := New(srv.URL)
	c.OnVersionMismatch = func(server, sdk string) {
		gotServer = server
		gotSDK = sdk
	}

	var result interface{}
	_ = c.do(context.Background(), http.MethodGet, "/health", nil, &result)

	if gotServer != "1.0.0" {
		t.Fatalf("expected server version 1.0.0, got %q", gotServer)
	}
	if gotSDK != SDKVersion {
		t.Fatalf("expected sdk version %s, got %q", SDKVersion, gotSDK)
	}
}

func TestVersionMismatchCalledOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Hermem-API-Version", "1.0.0")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var count atomic.Int32
	c := New(srv.URL)
	c.OnVersionMismatch = func(server, sdk string) {
		count.Add(1)
	}

	for i := 0; i < 5; i++ {
		var result interface{}
		_ = c.do(context.Background(), http.MethodGet, "/health", nil, &result)
	}

	if n := count.Load(); n != 1 {
		t.Fatalf("expected OnVersionMismatch called once, got %d", n)
	}
}

// TestAPIErrorReturnsRawBodyForTextPlainContentType covers the
// realistic case where the upstream returns an error response with
// Content-Type: text/plain; charset=utf-8. This is also what
// net/http's response-writer MIME-sNIFFER stamps on the wire when
// code calls WriteHeader without explicitly setting Content-Type and
// the body looks textual — so this test transitively covers both the
// "explicit text/plain" and "implicit text/plain via missing
// Content-Type" paths. (httptest.NewServer cannot easily produce a
// truly Content-Type-less response because of the sniffer, but in
// practice every Go HTTP server that "forgets" Content-Type delivers
// text/plain on the wire.)
//
// The Content-Type guard correctly treats text/plain as "not JSON"
// and falls through to the raw-body APIError path. Without this
// assertion the contract "non-JSON Content-Type ⇒ no JSON parse" was
// only implicitly covered by the HTML case (one observable family).
func TestAPIErrorReturnsRawBodyForTextPlainContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Explicit Set — not relying on net/http's MIME-sniffer
		// behaviour (which would set this anyway for textual bodies).
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("Bad Gateway"))
	}))
	defer srv.Close()

	c := New(srv.URL)
	var result interface{}
	err := c.do(context.Background(), http.MethodGet, "/anything", nil, &result)
	if err == nil {
		t.Fatal("expected error from do(), got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != http.StatusBadGateway {
		t.Errorf("expected StatusCode=502, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "Bad Gateway" {
		t.Errorf("expected Message equal to raw body \"Bad Gateway\", got %q", apiErr.Message)
	}
	if apiErr.Code != "" {
		t.Errorf("expected Code=\"\" (no JSON envelope to read), got %q", apiErr.Code)
	}
}

// TestAPIErrorReturnsRawBodyWhenJSONBodyIsInvalid covers the second
// valid fallthrough in the Content-Type guard: when the server DOES
// advertise application/json but the body is not valid JSON (or is
// valid JSON that lacks a populated "error" field), execution must
// fall through to the raw-body APIError path. This branch is verbally
// documented in the guard but — without this test — a maintainer
// could refactor the guard to "always JSON-parse if Content-Type
// starts with application/json" and silently lose the bad-body
// fallback.
func TestAPIErrorReturnsRawBodyWhenJSONBodyIsInvalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("not json {{{"))
	}))
	defer srv.Close()

	c := New(srv.URL)
	var result interface{}
	err := c.do(context.Background(), http.MethodGet, "/anything", nil, &result)
	if err == nil {
		t.Fatal("expected error from do(), got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected StatusCode=500, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "not json {{{" {
		t.Errorf("expected Message equal to raw body, got %q", apiErr.Message)
	}
	if apiErr.Code != "" {
		t.Errorf("expected Code=\"\" when JSON unmarshal produces no Error envelope, got %q", apiErr.Code)
	}
}

// TestAPIErrorReturnsRawHTMLBodyForNonJSONError is the regression test
// for the Content-Type guard added to (*Client).do. When the server (or
// an upstream proxy in front of it) returns a non-2xx response with a
// non-application/json Content-Type — the canonical example being an
// nginx/cloudflare 502 Bad Gateway HTML page — the client must surface
// the raw body in APIError.Message rather than (a) attempting JSON
// unmarshal on HTML (which always fails), or (b) producing a
// misleading zero-valued errResp envelope.
func TestAPIErrorReturnsRawHTMLBodyForNonJSONError(t *testing.T) {
	const htmlBody = "<html><body><h1>502 Bad Gateway</h1><p>cloudflare</p></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer srv.Close()

	c := New(srv.URL)
	var result interface{}
	err := c.do(context.Background(), http.MethodGet, "/anything", nil, &result)
	if err == nil {
		t.Fatal("expected error from do(), got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != http.StatusBadGateway {
		t.Errorf("expected StatusCode=502, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != htmlBody {
		t.Errorf("expected Message equal to raw HTML body\nwant: %q\ngot:  %q", htmlBody, apiErr.Message)
	}
	if apiErr.Code != "" {
		t.Errorf("expected Code=\"\" (no JSON envelope to read), got %q", apiErr.Code)
	}
}

// TestAPIErrorParsesJSONErrorEnvelope is the companion to the
// HTML-body test above: when the server *does* respond with
// Content-Type: application/json and a structured ErrorResponse
// envelope, the client must still surface the structured fields.
func TestAPIErrorParsesJSONErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid category","code":"invalid_category","field":"category"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	var result interface{}
	err := c.do(context.Background(), http.MethodPost, "/store", nil, &result)
	if err == nil {
		t.Fatal("expected error from do(), got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected StatusCode=400, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "invalid category" {
		t.Errorf("expected Message \"invalid category\", got %q", apiErr.Message)
	}
	if apiErr.Code != "invalid_category" {
		t.Errorf("expected Code \"invalid_category\", got %q", apiErr.Code)
	}
	if apiErr.Field != "category" {
		t.Errorf("expected Field \"category\", got %q", apiErr.Field)
	}
}

func TestParseMajor(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"0.3.0", 0},
		{"1.0.0", 1},
		{"2.1.3", 2},
		{"", 0},
		{"abc", 0},
	}
	for _, tt := range tests {
		if got := parseMajor(tt.input); got != tt.want {
			t.Errorf("parseMajor(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
