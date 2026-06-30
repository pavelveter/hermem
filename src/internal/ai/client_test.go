package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
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
