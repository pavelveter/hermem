package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestRegisterPprof_DisabledByDefault verifies that RegisterPprof is a
// no-op when HERMEM_PPROF_ENABLED is unset or set to anything other
// than the canonical "1" — production-safe by default.
func TestRegisterPprof_DisabledByDefault(t *testing.T) {
	t.Setenv("HERMEM_PPROF_ENABLED", "")
	mux := http.NewServeMux()
	RegisterPprof(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	// Each pprof path must 404 — the mux never registered handlers.
	for _, path := range []string{
		"/debug/pprof/",
		"/debug/pprof/cmdline",
		"/debug/pprof/profile",
		"/debug/pprof/symbol",
		"/debug/pprof/trace",
	} {
		resp, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: got status %d, want 404", path, resp.StatusCode)
		}
	}
}

// TestRegisterPprof_WrongEnvValue ensures non-"1" values do NOT enable
// the endpoints. "true" / "yes" / "on" are intentionally rejected —
// the contract is exact-match so a typo or shell-expansion accident
// cannot accidentally expose /debug/pprof in production.
func TestRegisterPprof_WrongEnvValue(t *testing.T) {
	for _, val := range []string{"true", "yes", "on", "0", "enabled", "TRUE"} {
		t.Run(val, func(t *testing.T) {
			t.Setenv("HERMEM_PPROF_ENABLED", val)
			mux := http.NewServeMux()
			RegisterPprof(mux)
			ts := httptest.NewServer(mux)
			t.Cleanup(ts.Close)

			resp, err := ts.Client().Get(ts.URL + "/debug/pprof/")
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("HERMEM_PPROF_ENABLED=%q: got %d, want 404", val, resp.StatusCode)
			}
		})
	}
}

// TestRegisterPprof_EnabledWhenFlagSet verifies that setting the env
// flag to exactly "1" registers all five pprof endpoints. We use the
// /debug/pprof/cmdline endpoint as the smoke check — it returns
// os.Args immediately without any timing-sensitive work.
func TestRegisterPprof_EnabledWhenFlagSet(t *testing.T) {
	t.Setenv("HERMEM_PPROF_ENABLED", "1")
	mux := http.NewServeMux()
	RegisterPprof(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	// /debug/pprof/cmdline returns newline-joined os.Args as text.
	resp, err := ts.Client().Get(ts.URL + "/debug/pprof/cmdline")
	if err != nil {
		t.Fatalf("GET cmdline: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cmdline: got %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("cmdline body empty")
	}
	// /debug/pprof/symbol accepts a POST of symbol->addr pairs and
	// returns the resolved names. Hit it with an empty POST to verify
	// the handler is wired and returns 200 (or an empty body).
	resp2, err := ts.Client().Post(ts.URL+"/debug/pprof/symbol", "application/octet-stream", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST symbol: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("symbol: got %d, want 200", resp2.StatusCode)
	}
}

// TestRegisterPprof_IntegrationSmoke is the gated integration check.
// Skip when the operator hasn't explicitly opted in (running unit tests
// with random env state would surprise callers — pprof endpoints
// expose process internals). When HERMEM_PPROF_TEST_INTEGRATION=1,
// boot a real httptest server and verify the pprof index page renders
// the expected profile names. The /debug/pprof/ page is an HTML listing
// so we can validate without parsing protobuf.
//
// We deliberately do NOT test /debug/pprof/profile or /debug/pprof/trace
// here — both are timing-sensitive (CPU profile sleeps 30s by default;
// execution trace is similar). The index + cmdline + symbol checks
// are sufficient to prove the handlers are wired and responding.
func TestRegisterPprof_IntegrationSmoke(t *testing.T) {
	if os.Getenv("HERMEM_PPROF_TEST_INTEGRATION") != "1" {
		t.Skip("set HERMEM_PPROF_TEST_INTEGRATION=1 to run pprof integration smoke")
	}
	t.Setenv("HERMEM_PPROF_ENABLED", "1")
	mux := http.NewServeMux()
	RegisterPprof(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	resp, err := ts.Client().Get(ts.URL + "/debug/pprof/")
	if err != nil {
		t.Fatalf("GET index: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index: got %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	// pprof.Index renders each profile name as a hyperlink. Verify
	// the names we care about are present.
	for _, name := range []string{"profile", "symbol", "trace", "goroutine", "heap", "allocs", "threadcreate", "cmdline"} {
		if !strings.Contains(string(body), name) {
			t.Errorf("index missing profile name %q", name)
		}
	}
}
