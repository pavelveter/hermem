package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	"github.com/pavelveter/hermem/src/internal/config"

	"github.com/pavelveter/hermem/src/internal/auth"
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

// TestRuntimeMiddleware_BindsSnapshotAndGetRuntimeReturnsIt — inner
// handler must observe the SAME *Env snapshot captured at request
// entry, even if the EnvManager's value is swapped mid-request. Locks
// the contract from out.txt § 4.1: handlers read the generation they
// entered with, never retroactively update on concurrent Reload.
func TestRuntimeMiddleware_BindsSnapshotAndGetRuntimeReturnsIt(t *testing.T) {
	original := &clienv.Env{
		Ctx:   t.Context(),
		Cfg:   &config.Config{},
		Build: clienv.BuildInfo{Version: "1.0.0"},
	}
	mgr := clienv.NewEnvManager(original)

	var seen *clienv.Env
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = GetRuntime(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := RuntimeMiddleware(mgr, silentLogger)(inner)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	h.ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%q)", rr.Code, rr.Body.String())
	}
	if seen == nil {
		t.Fatal("GetRuntime returned nil; middleware did not bind *Env to ctx")
	}
	if seen != original {
		t.Fatalf("snapshot pointer: want original, got different (identity matters per out.txt § 4.1)")
	}
	if seen.Build.Version != "1.0.0" {
		t.Fatalf("Build.Version: want 1.0.0, got %q (pass-by-pointer must reach handler)", seen.Build.Version)
	}
}

// TestRuntimeMiddleware_MidRequestReloadDoesNotRetroactivelySwap —
// assertion: a Reload fired between the middleware snapshot and the
// inner handler observing it does NOT retroactively change the
// snapshot bound to r.Context(). The handler sees the *Env it
// entered with.
//
// We use mgr.Set (not Reload) here deliberately: Set bypasses
// cfg.Validate, so the test isolates snapshot semantics from
// config-validation behaviour (covered separately by cli/env tests).
func TestRuntimeMiddleware_MidRequestReloadDoesNotRetroactivelySwap(t *testing.T) {
	gen1 := &clienv.Env{Ctx: t.Context(), Cfg: &config.Config{}, Build: clienv.BuildInfo{Version: "1"}}
	gen2 := &clienv.Env{Ctx: t.Context(), Cfg: &config.Config{}, Build: clienv.BuildInfo{Version: "2"}}
	mgr := clienv.NewEnvManager(gen1)

	var seenBuild string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate: a SIGHUP-arrived Reload has swapped the manager's value
		// BEFORE the inner handler reads ctx. We inline the swap (instead
		// of spawning a goroutine) for deterministic test ordering.
		mgr.Set(gen2)
		seen := GetRuntime(r.Context())
		if seen == nil {
			t.Fatal("GetRuntime nil after middleware")
		}
		// Identity check — locks the § 4.1 contract that the snapshot
		// bound at request entry is the SAME pointer the handler reads.
		if seen != gen1 {
			t.Fatalf("identity: want gen1 (entry-time snapshot), got different pointer — stale-rot regression")
		}
		seenBuild = seen.Build.Version
		w.WriteHeader(http.StatusOK)
	})
	h := RuntimeMiddleware(mgr, silentLogger)(inner)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	h.ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rr.Code)
	}
	if seenBuild != "1" {
		t.Fatalf("snapshot generation: want gen1 (was bound at entry), got %q", seenBuild)
	}
	if mgr.Get() != gen2 {
		t.Fatal("mgr.Get() after Set: want gen2 to confirm Set actually happened")
	}
}

// TestRuntimeMiddleware_NilManagerPanics — panic at wrap-time, fail-fast
// on misconfigured boot.
func TestRuntimeMiddleware_NilManagerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("nil EnvManager did not panic — fail-fast contract regressed")
		}
	}()
	_ = RuntimeMiddleware(nil, silentLogger)(http.NotFoundHandler())
}

// silentLogger is reused across the RuntimeMiddleware / GetRuntime
// tests. io.Discard + silent handler keeps test output clean even
// when assertions intentionally exercise error paths.
var silentLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// --- AuthMiddleware ---

func TestAuthMiddleware_NoAuthEnabled_Passes(t *testing.T) {
	mgr := clienv.NewEnvManager(&clienv.Env{
		Ctx: t.Context(),
		Cfg: &config.Config{},
	})
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RuntimeMiddleware(mgr, silentLogger)(AuthMiddleware()(inner))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/search", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200 (no auth), got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAuthMiddleware_HealthBypass(t *testing.T) {
	mgr := clienv.NewEnvManager(&clienv.Env{
		Ctx: t.Context(),
		Cfg: &config.Config{APIKey: "secret"},
	})
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RuntimeMiddleware(mgr, silentLogger)(AuthMiddleware()(inner))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health bypass: want 200, got %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/health/ready", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health/ready bypass: want 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_ValidKey_ReadScope(t *testing.T) {
	mgr := clienv.NewEnvManager(&clienv.Env{
		Ctx: t.Context(),
		Cfg: &config.Config{
			APIKeys: []auth.Key{{Value: "key-a", Scope: auth.ScopeRead}},
		},
	})
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RuntimeMiddleware(mgr, silentLogger)(AuthMiddleware()(inner))
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/search", nil)
	r.Header.Set("X-API-Key", "key-a")
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("valid key read scope: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAuthMiddleware_InsufficientScope_Returns403(t *testing.T) {
	mgr := clienv.NewEnvManager(&clienv.Env{
		Ctx: t.Context(),
		Cfg: &config.Config{
			APIKeys: []auth.Key{{Value: "key-read", Scope: auth.ScopeRead}},
		},
	})
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RuntimeMiddleware(mgr, silentLogger)(AuthMiddleware()(inner))
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/ingest", nil)
	r.Header.Set("X-API-Key", "key-read")
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("insufficient scope: want 403, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "insufficient_scope") {
		t.Fatalf("body: want insufficient_scope, got %q", rr.Body.String())
	}
}

func TestAuthMiddleware_AdminCanAccessAll(t *testing.T) {
	mgr := clienv.NewEnvManager(&clienv.Env{
		Ctx: t.Context(),
		Cfg: &config.Config{
			APIKeys: []auth.Key{{Value: "key-admin", Scope: auth.ScopeAdmin}},
		},
	})
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RuntimeMiddleware(mgr, silentLogger)(AuthMiddleware()(inner))
	for _, path := range []string{"/search", "/ingest", "/admin/re-embed"} {
		rr := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-API-Key", "key-admin")
		h.ServeHTTP(rr, r)
		if rr.Code != http.StatusOK {
			t.Fatalf("admin on %s: want 200, got %d", path, rr.Code)
		}
	}
}

func TestAuthMiddleware_MissingKey_Returns401(t *testing.T) {
	mgr := clienv.NewEnvManager(&clienv.Env{
		Ctx: t.Context(),
		Cfg: &config.Config{
			APIKeys: []auth.Key{{Value: "secret", Scope: auth.ScopeAdmin}},
		},
	})
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RuntimeMiddleware(mgr, silentLogger)(AuthMiddleware()(inner))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/ingest", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("missing key: want 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_WriteScopeCanRead(t *testing.T) {
	mgr := clienv.NewEnvManager(&clienv.Env{
		Ctx: t.Context(),
		Cfg: &config.Config{
			APIKeys: []auth.Key{{Value: "key-write", Scope: auth.ScopeWrite}},
		},
	})
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RuntimeMiddleware(mgr, silentLogger)(AuthMiddleware()(inner))
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/search", nil)
	r.Header.Set("X-API-Key", "key-write")
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("write scope on read endpoint: want 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_LegacySingleKey_ScopeAdmin(t *testing.T) {
	mgr := clienv.NewEnvManager(&clienv.Env{
		Ctx: t.Context(),
		Cfg: &config.Config{APIKey: "legacy-secret"},
	})
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RuntimeMiddleware(mgr, silentLogger)(AuthMiddleware()(inner))
	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/re-embed", nil)
	r.Header.Set("X-API-Key", "legacy-secret")
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("legacy key admin: want 200, got %d", rr.Code)
	}
}

// TestRuntimeMiddleware_NilSnapshotReturns500 — manager was empty (no
// NewEnvManager initial value, no Reload yet). Reject the request
// rather than dereferencing nil inside the inner handler.
func TestRuntimeMiddleware_NilSnapshotReturns500(t *testing.T) {
	mgr := clienv.NewEnvManager(nil)
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	})
	h := RuntimeMiddleware(mgr, silentLogger)(inner)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	h.ServeHTTP(rr, r)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status: want 500, got %d (empty manager must reject)", rr.Code)
	}
	if called {
		t.Fatal("inner handler called despite empty manager; middleware gate failed")
	}
}

// TestGetRuntime_ReturnsNilOnUnwrappedCtx — GetRuntime returns nil when
// the request context did not pass through RuntimeMiddleware (e.g. an
// internal test handler built without the full chain). Without this
// test the unwrapped-path would be uncovered — every other test wraps
// the request through SOME middleware and so does not exercise the
// nil-extractor branch.
func TestGetRuntime_ReturnsNilOnUnwrappedCtx(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("GetRuntime must not panic on unwrapped ctx, got %v", r)
		}
	}()
	r := httptest.NewRequest("GET", "/x", nil)
	if got := GetRuntime(r.Context()); got != nil {
		t.Fatalf("want nil for un-middleware'd ctx, got %v", got)
	}
}

// --- APIVersionMiddleware ---

func TestAPIVersionMiddleware_SetsHeader(t *testing.T) {
	h := APIVersionMiddleware("0.3.0")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))

	if got := rr.Header().Get("X-Hermem-API-Version"); got != "0.3.0" {
		t.Fatalf("want X-Hermem-API-Version=0.3.0, got %q", got)
	}
}

func TestAPIVersionMiddleware_EmptyVersion(t *testing.T) {
	h := APIVersionMiddleware("")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))

	if got := rr.Header().Get("X-Hermem-API-Version"); got != "" {
		t.Fatalf("want empty X-Hermem-API-Version, got %q", got)
	}
}
