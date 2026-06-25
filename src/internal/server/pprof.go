package server

import (
	"net/http"
	"net/http/pprof"
	"os"
)

// RegisterPprof wires Go's stdlib /debug/pprof/* handlers onto mux when
// the HERMEM_PPROF_ENABLED env var is exactly "1". Off by default — the
// daemon must opt in for ad-hoc profiling in production.
//
// SECURITY: This gate is INTENTIONALLY a single env flag with no auth.
// Do not add an API-key check or bind check here — the env flag is the
// only access control. Operators flip the flag when they need a profile
// and unset it when they're done. Production deployments leave the
// variable unset so /debug/pprof/* returns 404 (no handlers registered).
//
// WARNING: pprof endpoints expose process internals (memory layout,
// goroutine stacks, runtime symbols). Do not enable on public network
// listeners. Bind to a trusted interface or run behind a reverse-proxy
// allowlist if exposing externally is required.
//
// Each handler is registered individually rather than via
// http.DefaultServeMux so a caller-provided mux stays the single source
// of routing truth.
func RegisterPprof(mux *http.ServeMux) {
	if os.Getenv("HERMEM_PPROF_ENABLED") != "1" {
		return
	}
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}
