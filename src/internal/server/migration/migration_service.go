// Package migration hosts the HTTP transport for the migration /
// schema domain.
//
// PHASE 3.2 introduces this shell as part of the god-object
// demolition pattern established by PHASE 2.x + PHASE 3.1. The shell
// exposes 4 NEW HTTP routes that previously had no HTTP surface at
// all (only CLI subcommands):
//   - GET  /db/migrate   — applied/pending migration list
//   - POST /db/rollback  — remove last applied migration
//   - GET  /db/verify    — checksum integrity check
//   - GET  /db/schema    — schema fingerprint comparison
package migration

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	migrsvc "github.com/pavelveter/hermem/src/internal/migration"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService wires the migration domain to the HTTP mux.
//
// Three deps:
//   - Svc     — domain Service (Status / Rollback / Verify / Schema)
//   - Metrics — counter increments; never held by the domain Service
//   - Refs    — per-request schema source so SIGHUP reloads apply
//     without reconstructing the shell. Only Schema uses it (the
//     other three are pure SQL reads with no schema awareness).
type HTTPService struct {
	Svc     *migrsvc.Service
	Metrics *metrics.Metrics
	Refs    *serverstate.Ref
}

// New constructs an HTTPService. All three deps required.
func New(svc *migrsvc.Service, m *metrics.Metrics, refs *serverstate.Ref) *HTTPService {
	return &HTTPService{Svc: svc, Metrics: m, Refs: refs}
}

// Routes returns the URL → handler mapping.
//
// 4 endpoints — all new in PHASE 3.2 (no migration routes existed
// before this shell).
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/db/migrate":  s.HandleMigrationStatus,
		"/db/rollback": s.HandleMigrationRollback,
		"/db/verify":   s.HandleMigrationVerify,
		"/db/schema":   s.HandleSchemaFingerprint,
	}
}

// HandleMigrationStatus — GET /db/migrate. Returns the raw
// `[]store.MigStatus` as JSON. JSON tags on store.MigStatus
// (added in PHASE 3.2) provide snake_case keys: name, applied,
// applied_at (omitempty so non-applied rows omit the field).
func (s *HTTPService) HandleMigrationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	status, err := s.Svc.Status(r.Context())
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, status)
}

// HandleMigrationRollback — POST /db/rollback. Calls
// migration.Service.Rollback; returns `{rolled_back: <name>}`.
// Empty rollback (no applied migrations) returns
// `{rolled_back: ""}` so HTTP clients can distinguish "no-op
// success" from "rolled-back X" by reading the body field.
func (s *HTTPService) HandleMigrationRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name, err := s.Svc.Rollback(r.Context())
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"rolled_back": name})
}

// HandleMigrationVerify — GET /db/verify. Returns the integrity
// envelope `{integrity_ok: bool, mismatches: [...]}`.
//
// Status code is ALWAYS 200 on a successful read — even when
// mismatches are present. The reason: `db verify` is a read, not
// a write; the caller decides what to do with the mismatch data.
// CLI exit-1 semantics (compare pre-PHASE-3.2 cli/db/verify) are
// owned by the CLI handler, not the HTTP shape. This matches
// /graph/verify (PHASE 3.1) where pass=false also returns 200 so
// the dashboard can read the report and decide red/green on its
// own.
func (s *HTTPService) HandleMigrationVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	mismatches, err := s.Svc.Verify(r.Context())
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"integrity_ok": len(mismatches) == 0,
		"mismatches":   mismatches,
	})
}

// HandleSchemaFingerprint — GET /db/schema. Returns the
// migration.SchemaReport envelope `{stored, current, drift_detected}`.
// Schema is loaded per-request from Refs so SIGHUP-driven schema
// reloads apply without reconstructing the shell.
func (s *HTTPService) HandleSchemaFingerprint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	state := s.Refs.Load()
	if state == nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, "no server state")
		return
	}
	report, err := s.Svc.Schema(r.Context(), state.Schema)
	if err != nil {
		s.Metrics.IncErr()
		httputil.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusOK, report)
}
