// Package migration hosts the transport-agnostic migration / schema
// domain service — read-only inspection of applied migrations, the
// schema fingerprint, and the post-apply checksum integrity check.
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
type Service struct {
	db *sql.DB
}

// NewService constructs a Service. db must be non-nil.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// Status returns the applied/pending state of every embedded
// migration file in lexicographic order.
func (s *Service) Status(_ context.Context) ([]store.MigStatus, error) {
	return store.MigrationStatus(s.db)
}

// Rollback removes the last-applied migration. When target is
// non-empty, rolls back every migration applied after (and not
// including) the target version. No actual schema reverse — rows
// are DELETEd from `schema_migrations` and `migration_checksums`;
// the SQL schema itself is NOT reversed.
func (s *Service) Rollback(_ context.Context, target string) (string, error) {
	name, err := store.RollbackMigration(s.db, target)
	if err != nil {
		return "", fmt.Errorf("migration rollback: %w", err)
	}
	return name, nil
}

// Verify returns every migration whose stored checksum diverges
// from the current FNV-1a hash of the embedded migration file.
// Empty result → no integrity drift.
func (s *Service) Verify(_ context.Context) ([]store.MigMismatch, error) {
	return store.VerifyMigrationIntegrity(s.db)
}

// Run applies every pending migration in lexicographic order and
// returns the post-apply status snapshot.
func (s *Service) Run(ctx context.Context) ([]store.MigStatus, error) {
	if err := store.RunMigrations(s.db); err != nil {
		return nil, fmt.Errorf("migration apply: %w", err)
	}
	return s.Status(ctx)
}

// DryRun returns the list of pending migrations (not yet applied)
// with their SHA-256 checksums, without applying anything.
func (s *Service) DryRun(_ context.Context) ([]store.MigStatus, error) {
	status, err := store.MigrationStatus(s.db)
	if err != nil {
		return nil, fmt.Errorf("dry-run: %w", err)
	}
	pending := make([]store.MigStatus, 0, len(status))
	for _, m := range status {
		if !m.Applied {
			pending = append(pending, m)
		}
	}
	return pending, nil
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
