package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestHTTPClient_DoPOSTHappyPath pins the success path: 2xx response is
// decoded into dst, Content-Type is application/json on the wire, and the
// request body is the JSON-marshalled reqBody. This is the path all 6
// AI clients (Ollama/OpenAI embedders + extractors + rerankers) take on
// the happy case.
func TestHTTPClient_DoPOSTHappyPath(t *testing.T) {
	gotContentType := ""
	gotRawBody := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotRawBody = string(b)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"hello": "world"})
	}))
	defer srv.Close()

	c := newHTTPClient(srv.URL, "", 5*time.Second, 1)
	var resp map[string]string
	if err := c.doPOST(context.Background(), "/api/test", map[string]string{"k": "v"}, &resp); err != nil {
		t.Fatalf("doPOST: %v", err)
	}
	if resp["hello"] != "world" {
		t.Fatalf("decoded body: want {hello:world}, got %v", resp)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type: want application/json, got %q", gotContentType)
	}
	if !strings.Contains(gotRawBody, `"k":"v"`) {
		t.Fatalf("request body: want JSON {\"k\":\"v\"}, got %q", gotRawBody)
	}
}

// TestHTTPClient_DoPOSTNon200 pins the error-format path: non-2xx
// surfaces as fmt.Errorf("%d: %s", status, body) so callers can wrap their
// own domain prefix and preserve the underlying status text.
func TestHTTPClient_DoPOSTNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	c := newHTTPClient(srv.URL, "", 5*time.Second, 1)
	var resp map[string]string
	err := c.doPOST(context.Background(), "/api/test", nil, &resp)
	if err == nil {
		t.Fatal("expected error on 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error format: want \"404: not found\", got %q", err.Error())
	}
}

// TestHTTPClient_DoPOSTAuthHeaderWithKey pins the apiKey-on path: when
// apiKey is non-empty, the Authorization: Bearer <key> header is attached.
// Captured via a per-test closure so a future t.Parallel addition cannot
// race on shared state.
func TestHTTPClient_DoPOSTAuthHeaderWithKey(t *testing.T) {
	gotAuth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	c := newHTTPClient(srv.URL, "sk-test-key", 5*time.Second, 1)
	var resp map[string]string
	if err := c.doPOST(context.Background(), "/x", nil, &resp); err != nil {
		t.Fatalf("doPOST: %v", err)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Fatalf("Authorization: want \"Bearer sk-test-key\", got %q", gotAuth)
	}
}

// TestHTTPClient_DoPOSTAuthHeaderNoKey pins the apiKey-off path (Ollama):
// the Authorization header is NOT attached when apiKey is empty.
func TestHTTPClient_DoPOSTAuthHeaderNoKey(t *testing.T) {
	gotAuthSeen := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthSeen = r.Header.Get("Authorization") != ""
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	c := newHTTPClient(srv.URL, "", 5*time.Second, 1)
	var resp map[string]string
	if err := c.doPOST(context.Background(), "/x", nil, &resp); err != nil {
		t.Fatalf("doPOST: %v", err)
	}
	if gotAuthSeen {
		t.Fatalf("Authorization: want absent (no apiKey), got present")
	}
}

// TestHTTPClient_DoPOSTPathConcatenation pins the baseURL contract:
// newHTTPClient trims any trailing "/" once at construction, so callers
// concatenating "/api/foo" to a baseURL like "http://x/" never produces
// "http://x//api/foo" or "http://x/" + "/api/foo" → "//api/foo" on the
// downstream side. The captured request path must equal /api/foo.
func TestHTTPClient_DoPOSTPathConcatenation(t *testing.T) {
	gotPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	// httptest.URL never has a trailing /, so we manually bolt one on
	// to exercise the trim path. The recorded path on the server should
	// still be exactly /api/foo with no // artifacts.
	trimmedURL := srv.URL + "/"
	c := newHTTPClient(trimmedURL, "", 5*time.Second, 1)
	var resp map[string]string
	if err := c.doPOST(context.Background(), "/api/foo", nil, &resp); err != nil {
		t.Fatalf("doPOST: %v", err)
	}
	if gotPath != "/api/foo" {
		t.Fatalf("request path: want /api/foo (single slash), got %q — trailing-slash trim likely broken", gotPath)
	}
}

// TestHTTPClient_DoPOSTRetryReplaysBody pins the GetBody contract: when
// ResilientClient.Do needs to retry (5xx response from attempt #1), doPOST
// must have attached a GetBody closure so the captured body bytes can be
// replayed on attempt #2. Without this, every retry would fail with
// `GetBody returns nil` once the body was consumed by the first attempt.
//
// The handler returns 500 on the first request, then 200 + decoded JSON
// on the second. If the second attempt receives an empty body the JSON
// decode will fail; the assertion on body-on-second-attempt vs body-on-
// second-expected pins both the body-replay AND the happy-path-decode.
func TestHTTPClient_DoPOSTRetryReplaysBody(t *testing.T) {
	var calls atomic.Int32
	var bodyOnSecondAttempt string
	expectedBodyBytes, _ := json.Marshal(map[string]string{"k": "v"})
	expectedBody := string(expectedBodyBytes)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		if n == 2 {
			bodyOnSecondAttempt = string(b)
		}
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	c := newHTTPClient(srv.URL, "", 5*time.Second, 3) // 1 + 2 retries
	var resp map[string]string
	if err := c.doPOST(context.Background(), "/retry", map[string]string{"k": "v"}, &resp); err != nil {
		t.Fatalf("doPOST: %v (retry contract likely broken)", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls: want 2 (1 retry), got %d", got)
	}
	if bodyOnSecondAttempt != expectedBody {
		t.Fatalf("body-on-second-attempt: want %q (replayed via GetBody), got %q (GetBody likely dropped)", expectedBody, bodyOnSecondAttempt)
	}
	if resp["ok"] != "yes" {
		t.Fatalf("decoded body from second attempt: want {ok:yes}, got %v", resp)
	}
}

// TestHTTPClient_DoPOSTTimeoutPropagates pins the timeout contract: when
// newHTTPClient is constructed with a timeout and the upstream handler hangs
// past it, doPOST must surface a deadline-exceeded error rather than block
// forever. Otherwise callers would never observe a hung Ollama/OpenAI host
// and a collection pool could starve waiting for an answer.
func TestHTTPClient_DoPOSTTimeoutPropagates(t *testing.T) {
	never := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-never // hang until the test releases; goroutine leaks per test if we leak never
	}))
	defer func() {
		close(never) // release the hung goroutine
		srv.Close()
	}()

	// Tight timeout — 50ms — so the test runs in well under a second.
	c := newHTTPClient(srv.URL, "", 50*time.Millisecond, 1)
	var resp map[string]string
	start := time.Now()
	err := c.doPOST(context.Background(), "/hang", nil, &resp)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error from hung handler, got nil")
	}
	// Assert the error is specifically a net.Error.Timeout() — a generic
	// `err != nil` check would let connection-refused / EOF pass, which
	// don't prove the http.Client timeout fired.
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected net.Error with Timeout()==true, got %T: %v (timeout contract may not be asserting specifically)", err, err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("doPOST took %v on a 50ms timeout — timeout contract likely broken", elapsed)
	}
}
