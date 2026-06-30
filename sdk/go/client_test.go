package hermem

import (
	"context"
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
