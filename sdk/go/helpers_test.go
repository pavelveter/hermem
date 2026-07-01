package hermem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// runSDKCall spins up an httptest server that asserts the request method
// and path, then returns the canned response. The run callback receives
// a Client connected to the mock server.
//
//	wantMethod, wantPath  — request assertions
//	wantStatus            — HTTP status code to return
//	respBody              — response body. If a string, used verbatim
//	                       (raw JSON); otherwise JSON-marshalled.
//	run                   — closure that exercises the SDK method.
func runSDKCall(t *testing.T, wantMethod, wantPath string, wantStatus int, respBody any, run func(c *Client)) {
	t.Helper()
	var body []byte
	switch v := respBody.(type) {
	case nil:
		body = nil
	case string:
		body = []byte(v)
	default:
		var err error
		body, err = json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal resp body: %v", err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != wantMethod {
			t.Errorf("method: got %q, want %q", r.Method, wantMethod)
		}
		if r.URL.Path != wantPath {
			t.Errorf("path: got %q, want %q", r.URL.Path, wantPath)
		}
		if body != nil {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(wantStatus)
		if body != nil {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(srv.Close)
	run(New(srv.URL))
}

// runSDKCallWithQuery is like runSDKCall but also asserts the request
// query string. Tests use it for endpoints that build query params
// from arguments (e.g. ?min_size=2, ?limit=10).
func runSDKCallWithQuery(t *testing.T, wantMethod, wantPath, wantQuery string, wantStatus int, respBody any, run func(c *Client)) {
	t.Helper()
	var body []byte
	switch v := respBody.(type) {
	case nil:
		body = nil
	case string:
		body = []byte(v)
	default:
		var err error
		body, err = json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal resp body: %v", err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != wantMethod {
			t.Errorf("method: got %q, want %q", r.Method, wantMethod)
		}
		if r.URL.Path != wantPath {
			t.Errorf("path: got %q, want %q", r.URL.Path, wantPath)
		}
		if got := r.URL.RawQuery; got != wantQuery {
			t.Errorf("query: got %q, want %q", got, wantQuery)
		}
		if body != nil {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(wantStatus)
		if body != nil {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(srv.Close)
	run(New(srv.URL))
}
