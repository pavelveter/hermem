package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRecoveryMiddleware_PanicConvertsTo500 — a handler that panics must
// produce a 500, not crash the test. Recovery is the last line of defense
// against bugs in a downstream handler leaking to the client.
func TestRecoveryMiddleware_PanicConvertsTo500(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("recovery failed: leaked panic %v", r)
		}
	}()
	h := RecoveryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: want 500, got %d", rr.Code)
	}
}

// TestRecoveryMiddleware_HappyPathPasses — a healthy handler is unaffected.
func TestRecoveryMiddleware_HappyPathPasses(t *testing.T) {
	h := RecoveryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
	if rr.Code != http.StatusCreated || rr.Body.String() != "ok" {
		t.Fatalf("happy: want 201/ok, got %d/%q", rr.Code, rr.Body.String())
	}
}

// TestRequestIDMiddleware_EchoesIncomingHeader — caller-supplied
// X-Request-ID must be preserved verbatim, not regenerated.
func TestRequestIDMiddleware_EchoesIncomingHeader(t *testing.T) {
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("X-Request-ID", "abc-123")
	h.ServeHTTP(rr, r)
	if got := rr.Header().Get("X-Request-ID"); got != "abc-123" {
		t.Fatalf("request id echo: want abc-123, got %q", got)
	}
}

// TestRequestIDMiddleware_GeneratesWhenMissing — absent header → server
// picks a non-empty id (we don't check the format, only non-empty).
func TestRequestIDMiddleware_GeneratesWhenMissing(t *testing.T) {
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
	if got := rr.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("generated request id: want non-empty, got empty")
	}
}

// TestAPIKeyMiddleware_RejectsMissing — server has an api-key set; missing
// X-API-Key header must surface 401 with the canonical JSON envelope.
func TestAPIKeyMiddleware_RejectsMissing(t *testing.T) {
	h := APIKeyMiddleware("secret")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("body: want unauthorized envelope, got %q", rr.Body.String())
	}
}

// TestAPIKeyMiddleware_AcceptsMatching — correctly-set X-API-Key passes
// through to the inner handler.
func TestAPIKeyMiddleware_AcceptsMatching(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := APIKeyMiddleware("secret")(inner)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("X-API-Key", "secret")
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d", rr.Code)
	}
}

// TestAPIKeyMiddleware_EmptyKeyDisablesAuth — empty apiKey = auth off
// (dev default). Even a wrong header must pass.
func TestAPIKeyMiddleware_EmptyKeyDisablesAuth(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := APIKeyMiddleware("")(inner)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("X-API-Key", "anything")
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200 (auth off), got %d", rr.Code)
	}
}

// TestMaxBytesMiddleware_CapsRead — once the body exceeds the limit, the
// wrapped reader returns an error on the next Read. This is the contract
// the rest of the stack relies on for OOM defense.
func TestMaxBytesMiddleware_CapsRead(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err == nil {
			t.Errorf("expected error from capped body, got nil")
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	h := MaxBytesMiddleware(8)(inner)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/x", bytes.NewReader([]byte("hello world this is way too long")))
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200 (inner handler ran), got %d", rr.Code)
	}
}

// TestMaxBytesMiddleware_AllowsShortBody — bodies under the limit must
// pass through with no error.
func TestMaxBytesMiddleware_AllowsShortBody(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("unexpected read error: %v", err)
		}
		if string(b) != "hi" {
			t.Errorf("body bytes: want %q, got %q", "hi", string(b))
		}
		w.WriteHeader(http.StatusOK)
	})
	h := MaxBytesMiddleware(8)(inner)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/x", bytes.NewReader([]byte("hi")))
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rr.Code)
	}
}
