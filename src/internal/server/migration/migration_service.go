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
//
// §3.2 — embeds shared.BaseHTTPService; all 4 handlers route through
// s.Wrap so the IncErr + WriteError(500,...) boilerplate is collapsed.
package migration

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/httputil"
	migrsvc "github.com/pavelveter/hermem/src/internal/migration"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/server/shared"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// HTTPService wires the migration domain to the HTTP mux.
//
// Three deps (via the embedded BaseHTTPService + Svc):
//   - Svc     — domain Service (Status / Rollback / Verify / Schema)
//   - Metrics — counter increments; never held by the domain Service
//   - Refs    — per-request schema source so SIGHUP reloads apply
//     without reconstructing the shell. Only Schema uses it (the
//     other three are pure SQL reads with no schema awareness).
type HTTPService struct {
	Svc *migrsvc.Service
	shared.BaseHTTPService
}

// New constructs an HTTPService. All three deps required.
func New(svc *migrsvc.Service, m *metrics.Metrics, refs *serverstate.Ref) *HTTPService {
	return &HTTPService{
		Svc: svc,
		BaseHTTPService: shared.BaseHTTPService{
			Metrics: m,
			Refs:    refs,
		},
	}
}

// Routes returns the URL → handler mapping.
//
// §3.2 — all four handlers route through s.Wrap.
func (s *HTTPService) Routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/db/migrate":  s.Wrap(s.HandleMigrationStatus),
		"/db/rollback": s.Wrap(s.HandleMigrationRollback),
		"/db/verify":   s.Wrap(s.HandleMigrationVerify),
		"/db/schema":   s.Wrap(s.HandleSchemaFingerprint),
	}
}

// HandleMigrationStatus — GET /db/migrate. Returns the raw
// `[]store.MigStatus` as JSON.
//
// §3.2 — error-returning handler. The pre-§3.2 4-line
// IncErr+WriteError block became a single `return err`.
func (s *HTTPService) HandleMigrationStatus(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	status, err := s.Svc.Status(r.Context())
	if err != nil {
		return err
	}
	httputil.WriteJSON(w, http.StatusOK, status)
	return nil
}

// HandleMigrationRollback — POST /db/rollback. Calls
// migration.Service.Rollback; returns `{rolled_back: <name>}`.
// Empty rollback (no applied migrations) returns
// `{rolled_back: ""}` so HTTP clients can distinguish "no-op
// success" from "rolled-back X" by reading the body field.
func (s *HTTPService) HandleMigrationRollback(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	name, err := s.Svc.Rollback(r.Context(), "")
	if err != nil {
		return err
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"rolled_back": name})
	return nil
}

// HandleMigrationVerify — GET /db/verify. Status code is ALWAYS 200 on
// a successful read — even when mismatches are present (`db verify` is
// a read, not a write; the caller decides what to do with mismatches).
//
// §3.2 — error-returning handler.
func (s *HTTPService) HandleMigrationVerify(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	mismatches, err := s.Svc.Verify(r.Context())
	if err != nil {
		return err
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"integrity_ok": len(mismatches) == 0,
		"mismatches":   mismatches,
	})
	return nil
}

// HandleSchemaFingerprint — GET /db/schema.
//
// §3.2 — error-returning handler. The pre-§3.2 inline "no server
// state" 500 defense-in-depth branch is replaced by returning the
// errNoServerState sentinel that mapStatus maps to (500, msg);
// identical wire bytes.
func (s *HTTPService) HandleSchemaFingerprint(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}
	state := s.Refs.Load()
	if state == nil {
		return shared.ErrNoServerState
	}
	report, err := s.Svc.Schema(r.Context(), state.Schema)
	if err != nil {
		return err
	}
	httputil.WriteJSON(w, http.StatusOK, report)
	return nil
}

// HandleSchemaFingerprint's defensive state==nil branch returns
// shared.ErrNoServerState so that mapStatus routes to (500, "no
// server state") — identical wire bytes to the pre-§3.2 inline
// WriteError(500, ...) branch.
