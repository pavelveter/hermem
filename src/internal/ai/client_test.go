package ai

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResilientClient_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewResilientClient(nil, RetryPolicy{
		MaxAttempts: 3,
		Backoff:     []time.Duration{1 * time.Millisecond, 2 * time.Millisecond},
	})
	req, err := http.NewRequest("GET", srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := c.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
}

func TestResilientClient_Retries5xxThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewResilientClient(nil, RetryPolicy{
		MaxAttempts: 4,
		Backoff:     []time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond},
	})
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := c.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200 (after retried 5xx), got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("calls: want 3, got %d", got)
	}
}

func TestResilientClient_AttemptsExhaustedReturnsLastErr(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("down"))
	}))
	defer srv.Close()

	c := NewResilientClient(nil, RetryPolicy{
		MaxAttempts: 2,
		Backoff:     []time.Duration{1 * time.Millisecond},
	})
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := c.Do(t.Context(), req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected last error after 2×5xx")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("calls: want 2, got %d", got)
	}
}

func TestResilientClient_DefaultsKickInWhenZero(t *testing.T) {
	if DefaultBackoffs() == nil || len(DefaultBackoffs()) == 0 {
		t.Fatal("DefaultBackoffs must be populated so users get a sensible ladder by zero-value")
	}
	c := &ResilientClient{Inner: nil} // forces http.DefaultClient fallback
	req, _ := http.NewRequest("GET", "http://127.0.0.1:1/healthz", nil)
	// Don't care what happens — we just need the call to not panic and
	// prove the zero-value configuration path is reachable.
	_, _ = c.Do(t.Context(), req)
}

func TestResilientClient_CtxCancelAbortsRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewResilientClient(nil, RetryPolicy{
		MaxAttempts: 5,
		Backoff:     []time.Duration{50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond},
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // pre-cancelled
	req, _ := http.NewRequest("GET", srv.URL, nil)
	if _, err := c.Do(ctx, req); err == nil {
		t.Fatal("expected ctx.Canceled error")
	}
}

// --- C1.5 comprehensive tests ---

func TestResilientClient_Retries429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewResilientClient(nil, RetryPolicy{
		MaxAttempts: 3,
		Backoff:     []time.Duration{1 * time.Millisecond},
	})
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := c.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200 (after retried 429), got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("calls: want 2, got %d", got)
	}
}

func TestResilientClient_NonRetryable4xxReturnsImmediately(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := NewResilientClient(nil, RetryPolicy{
		MaxAttempts: 5,
		Backoff:     []time.Duration{1 * time.Millisecond},
	})
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := c.Do(t.Context(), req)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Do should not error on 400, got: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls: want 1 (no retry on 400), got %d", got)
	}
}

func TestResilientClient_MaxWallClockStopsRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewResilientClient(nil, RetryPolicy{
		MaxAttempts:   20,
		Backoff:       []time.Duration{50 * time.Millisecond},
		MaxWallClock:  80 * time.Millisecond,
	})
	req, _ := http.NewRequest("GET", srv.URL, nil)
	start := time.Now()
	resp, err := c.Do(t.Context(), req)
	elapsed := time.Since(start)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected error from wall-clock guard")
	}
	// Should stop well before 20 attempts × 50ms = 1s
	if elapsed > 500*time.Millisecond {
		t.Fatalf("wall-clock guard took %v — should have stopped around 80ms", elapsed)
	}
}

func TestResilientClient_NegativeMaxWallClockDisablesGuard(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 4 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewResilientClient(nil, RetryPolicy{
		MaxAttempts:  5,
		Backoff:      []time.Duration{1 * time.Millisecond},
		MaxWallClock: -1, // disabled
	})
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := c.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("calls: want 4, got %d", got)
	}
}

func TestResilientClient_CustomRetryableStatus(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503 — NOT in custom set
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewResilientClient(nil, RetryPolicy{
		MaxAttempts:    3,
		Backoff:        []time.Duration{1 * time.Millisecond},
		RetryableStatus: map[int]bool{429: true}, // only 429
	})
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := c.Do(t.Context(), req)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	// 503 should NOT be retried with custom set
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls: want 1 (503 not in custom RetryableStatus), got %d", got)
	}
}

func TestResilientClient_PrepareRequest_NilGetBodyPreservesOriginal(t *testing.T) {
	// Body content that survives both attempts
	bodyBytes := []byte("test-body")
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		b, _ := io.ReadAll(r.Body)
		if n == 1 {
			// First attempt: return 500 to trigger retry
			w.WriteHeader(http.StatusInternalServerError)
			// Verify body was present on first attempt
			if string(b) != "test-body" {
				t.Errorf("attempt 1 body: want %q, got %q", "test-body", string(b))
			}
			return
		}
		// Second attempt: return 200
		w.WriteHeader(http.StatusOK)
		// With nil GetBody, body should be the same (bytes.Reader rewound via GetBody)
		if string(b) != "test-body" {
			t.Errorf("attempt 2 body: want %q, got %q", "test-body", string(b))
		}
	}))
	defer srv.Close()

	c := NewResilientClient(nil, RetryPolicy{
		MaxAttempts: 3,
		Backoff:     []time.Duration{1 * time.Millisecond},
	})
	req, _ := http.NewRequest("POST", srv.URL, bytes.NewReader(bodyBytes))
	// GetBody is set by http.NewRequest for *bytes.Reader — verify it works
	resp, err := c.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
}

func TestResilientClient_PrepareRequest_FailedGetBodyReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewResilientClient(nil, RetryPolicy{
		MaxAttempts: 3,
		Backoff:     []time.Duration{1 * time.Millisecond},
	})
	req, _ := http.NewRequest("POST", srv.URL, nil)
	req.GetBody = func() (io.ReadCloser, error) {
		return nil, io.ErrUnexpectedEOF
	}
	resp, err := c.Do(t.Context(), req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected error from failed GetBody")
	}
	if !strings.Contains(err.Error(), "get body") {
		t.Fatalf("error should mention 'get body', got: %v", err)
	}
}

func TestClassifyResponse_Retryable(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Body:       io.NopCloser(strings.NewReader("body")),
	}
	retryable := map[int]bool{503: true}
	retry, err := classifyResponse(resp, retryable)
	if !retry {
		t.Fatal("expected retry=true for 503")
	}
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected error containing '503', got: %v", err)
	}
}

func TestClassifyResponse_NotRetryable(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("body")),
	}
	retryable := map[int]bool{503: true}
	retry, err := classifyResponse(resp, retryable)
	if retry {
		t.Fatal("expected retry=false for 200")
	}
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestResolvePolicy_DefaultsApplied(t *testing.T) {
	p := resolvePolicy(RetryPolicy{})
	if p.MaxAttempts != defaultMaxAttempts {
		t.Fatalf("MaxAttempts: want %d, got %d", defaultMaxAttempts, p.MaxAttempts)
	}
	if len(p.Backoff) != len(defaultBackoffs) {
		t.Fatalf("Backoff: want %d slots, got %d", len(defaultBackoffs), len(p.Backoff))
	}
	if len(p.RetryableStatus) != len(defaultRetryableStatus) {
		t.Fatalf("RetryableStatus: want %d entries, got %d", len(defaultRetryableStatus), len(p.RetryableStatus))
	}
	if p.MaxWallClock != defaultMaxWallClock {
		t.Fatalf("MaxWallClock: want %v, got %v", defaultMaxWallClock, p.MaxWallClock)
	}
}

func TestResolvePolicy_CustomValuesPreserved(t *testing.T) {
	custom := map[int]bool{418: true}
	p := resolvePolicy(RetryPolicy{
		MaxAttempts:    7,
		Backoff:        []time.Duration{10 * time.Millisecond},
		RetryableStatus: custom,
		MaxWallClock:   5 * time.Second,
	})
	if p.MaxAttempts != 7 {
		t.Fatalf("MaxAttempts: want 7, got %d", p.MaxAttempts)
	}
	if len(p.Backoff) != 1 {
		t.Fatalf("Backoff: want 1 slot, got %d", len(p.Backoff))
	}
	if !p.RetryableStatus[418] {
		t.Fatal("RetryableStatus: custom map not preserved")
	}
	if p.MaxWallClock != 5*time.Second {
		t.Fatalf("MaxWallClock: want 5s, got %v", p.MaxWallClock)
	}
}

// TestResilientClient_AttemptCapInvariant verifies the core property:
// Do never makes more HTTP calls than MaxAttempts, regardless of how
// the backoff ladder is configured. This is the critical safety
// guarantee — runaway retries would waste resources and risk
// overwhelming the provider.
func TestResilientClient_AttemptCapInvariant(t *testing.T) {
	tests := []struct {
		name       string
		maxAttempts int
		backoff    []time.Duration
	}{
		{"1 attempt, no backoff", 1, nil},
		{"1 attempt, long backoff", 1, []time.Duration{10 * time.Second}},
		{"3 attempts, short backoff", 3, []time.Duration{1 * time.Millisecond}},
		{"5 attempts, mixed backoff", 5, []time.Duration{1 * time.Millisecond, 5 * time.Millisecond}},
		{"10 attempts, default backoff", 10, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()

			c := NewResilientClient(nil, RetryPolicy{
				MaxAttempts:   tt.maxAttempts,
				Backoff:       tt.backoff,
				MaxWallClock:  -1, // disabled — rely on attempt cap only
			})
			req, _ := http.NewRequest("GET", srv.URL, nil)
			resp, err := c.Do(t.Context(), req)
			if resp != nil {
				resp.Body.Close()
			}
			if err == nil {
				t.Fatal("expected error from exhausted retries")
			}
			got := atomic.LoadInt32(&calls)
			if got != int32(tt.maxAttempts) {
				t.Fatalf("calls: want %d (exactly MaxAttempts), got %d", tt.maxAttempts, got)
			}
		})
	}
}
