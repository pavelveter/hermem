// Package migration hosts the transport-agnostic migration / schema
// domain service — read-only inspection of applied migrations, the
// schema fingerprint, and the post-apply checksum integrity check.
//
// PHASE 3.2 consolidates the four cli/db/* commands into one
// transport-agnostic Service. Pre-PHASE-3.2 each cli/db/{migrate,
// schema, verify, rollback}.go called the corresponding store.*
// function directly, never going through any service layer.
//
// OUT OF SCOPE (deliberately stays in store/):
//   - store.RunMigrations(db)         — apply pending migrations; called
//     from main.go at boot. Does not
//     belong on the HTTP request
//     surface (would need apply
//     transaction wrapping + per-stmt
//     idempotency logging in the new
//     pkg).
//   - store.StoreSchemaFingerprint(db) — overwrite the stored
//     fingerprint; called from
//     cli/serve.go's SIGHUP reload
//     loop after ReloadState. Same
//     reasoning — bootstrapping
//     mutation, not request-time
//     read.
//
// Both stay in `store/` because they are bootstrapping mutating
// hooks that fire outside the request lifecycle. They are NOT
// re-homed here.
//
// Mirrors the PHASE 3.x flat-pkg + transport-agnostic pattern:
// Service struct holds { db } only. Per-call args include `schema
// core.SchemaConfig` when schema-dependent (only the Schema
// method). No Refs/Metrics coupling — HTTP shell increments
// counters on its own; the domain service is pure.
package migration

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// Service is the transport-agnostic migration / schema domain service.
//
// One dep at construction — db — same as PHASE 2.3 contradiction +
// PHASE 3.1 graph. The three read-only methods (Status / Rollback /
// Verify) are pure SQL. Schema takes a per-call schema arg so a SIGHUP
// reload that swaps cfg.Schema doesn't require reconstructing.
//
// No Refs/Metrics coupling — the HTTP shell increments counters on
// its own; the domain service is pure.
type Service struct {
	db *sql.DB
}

// NewService constructs a Service. db must be non-nil.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// Status returns the applied/pending state of every embedded
// migration file in lexicographic order. Pre-PHASE-3.2 this was
// store.MigrationStatus; PHASE 3.2 owns it inside the migration
// service domain.
//
// Returns `[]store.MigStatus{}` (not nil) on empty result so the
// HTTP envelope emits `[]` not `null`. store.MigStatus now ships
// JSON tags (added in PHASE 3.2) so the wire shape matches the
// struct shape without an adapter — same precedent as
// core.ContradictionPair.
func (s *Service) Status(_ context.Context) ([]store.MigStatus, error) {
	status, err := store.MigrationStatus(s.db)
	if err != nil {
		return nil, fmt.Errorf("migration status: %w", err)
	}
	if status == nil {
		status = []store.MigStatus{}
	}
	return status, nil
}

// Rollback removes the last-applied migration (no actual schema
// reverse — the row is DELETEd from `schema_migrations` and the
// checksum row from `migration_checksums`; the SQL schema itself
// is NOT reversed). Returns the rolled-back name, or "" if no
// migrations exist. Matches pre-PHASE-3.2 cli/db/rollback
// semantics 1:1: the CLI prints "No migrations." on empty
// result, and the HTTP shell emits `{rolled_back: ""}`.
func (s *Service) Rollback(_ context.Context) (string, error) {
	name, err := store.RollbackMigration(s.db)
	if err != nil {
		return "", fmt.Errorf("migration rollback: %w", err)
	}
	return name, nil
}

// Verify returns every migration whose stored checksum diverges
// from the current FNV-1a hash of the embedded migration file.
// Empty result → no integrity drift. Returns
// `[]store.MigMismatch{}` (not nil) on clean empty so the envelope
// stays `[]`.
func (s *Service) Verify(_ context.Context) ([]store.MigMismatch, error) {
	mismatches, err := store.VerifyMigrationIntegrity(s.db)
	if err != nil {
		return nil, fmt.Errorf("migration verify: %w", err)
	}
	if mismatches == nil {
		mismatches = []store.MigMismatch{}
	}
	return mismatches, nil
}

// SchemaReport is the typed envelope returned by Schema. JSON tags
// live here (transport concern) and not in the store schema. The
// drift boolean derives from (stored != "") && (stored != current)
// — the first run returns stored == "" and drift_detected ==
// false (no prior stored fingerprint to drift from, so no drift
// to detect on first boot).
type SchemaReport struct {
	Stored        string `json:"stored"`
	Current       string `json:"current"`
	DriftDetected bool   `json:"drift_detected"`
}

// Schema compares the current schema fingerprint against the
// stored value in the meta table. On first run (no stored row) it
// inserts the current fingerprint and returns stored == "". Schema
// is per-call so SIGHUP-driven config reloads apply without
// reconstructing the service.
func (s *Service) Schema(_ context.Context, schema core.SchemaConfig) (SchemaReport, error) {
	stored, current, err := store.CheckSchemaFingerprint(s.db, schema)
	if err != nil {
		return SchemaReport{}, fmt.Errorf("schema fingerprint: %w", err)
	}
	return SchemaReport{
		Stored:        stored,
		Current:       current,
		DriftDetected: stored != "" && stored != current,
	}, nil
}
