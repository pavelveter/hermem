// Package migration hosts the transport-agnostic migration / schema
// domain service — read-only inspection of applied migrations, the
// schema fingerprint, and the post-apply checksum integrity check.
//
// OUT OF SCOPE (deliberately stays in store/):
//   - store.RunMigrations(db)         — apply pending migrations; called
//     from main.go at boot. Does not belong on the HTTP request surface.
//   - store.StoreSchemaFingerprint(db) — overwrite the stored fingerprint;
//     called from cli/serve.go's SIGHUP reload loop.
//
// Both stay in `store/` because they are bootstrapping mutating
// hooks that fire outside the request lifecycle.
//
// Implements core.Migrator — the minimal interface for migration ops.
package migration

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// Service is the transport-agnostic migration / schema domain service.
// Implements core.Migrator.
type Service struct {
	db *sql.DB
}

// Compile-time interface assertion.
var _ core.Migrator = (*Service)(nil)

// New constructs a Service. db must be non-nil.
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// Status returns the applied/pending state of every embedded
// migration file in lexicographic order.
func (s *Service) Status(_ context.Context) ([]core.MigrationStatus, error) {
	storeStatus, err := store.MigrationStatus(s.db)
	if err != nil {
		return nil, err
	}
	return toCoreStatus(storeStatus), nil
}

// Rollback removes the last-applied migration. When target is
// non-empty, rolls back every migration applied after (and not
// including) the target version.
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
func (s *Service) Verify(_ context.Context) ([]core.MigrationMismatch, error) {
	storeMismatches, err := store.VerifyMigrationIntegrity(s.db)
	if err != nil {
		return nil, err
	}
	return toCoreMismatch(storeMismatches), nil
}

// Run applies every pending migration in lexicographic order and
// returns the post-apply status snapshot.
func (s *Service) Run(ctx context.Context) ([]core.MigrationStatus, error) {
	if err := store.RunMigrations(s.db); err != nil {
		return nil, fmt.Errorf("migration apply: %w", err)
	}
	return s.Status(ctx)
}

// DryRun returns the list of pending migrations (not yet applied)
// with their SHA-256 checksums, without applying anything.
func (s *Service) DryRun(ctx context.Context) ([]core.MigrationStatus, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("dry-run: %w", err)
	}
	pending := make([]core.MigrationStatus, 0, len(status))
	for _, m := range status {
		if !m.Applied {
			pending = append(pending, m)
		}
	}
	return pending, nil
}

// SchemaReport is the typed envelope returned by Schema.
type SchemaReport struct {
	Stored        string `json:"stored"`
	Current       string `json:"current"`
	DriftDetected bool   `json:"drift_detected"`
}

// Schema compares the current schema fingerprint against the
// stored value in the meta table.
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

// SchemaFingerprint overwrites the stored schema fingerprint.
// This is a bootstrapping mutation (called from SIGHUP reload)
// and deliberately kept as a concrete method rather than on
// core.Migrator — it is not a read-only inspection op.
func (s *Service) SchemaFingerprint(_ context.Context, schema core.SchemaConfig) error {
	return store.StoreSchemaFingerprint(s.db, schema)
}

// --- type adapters (store → core) ---

func toCoreStatus(in []store.MigStatus) []core.MigrationStatus {
	out := make([]core.MigrationStatus, len(in))
	for i, s := range in {
		out[i] = core.MigrationStatus{
			Name:           s.Name,
			Applied:        s.Applied,
			AppliedAt:      s.AppliedAt,
			ChecksumSHA256: s.ChecksumSHA256,
			ChecksumMatch:  s.ChecksumMatch,
		}
	}
	return out
}

func toCoreMismatch(in []store.MigMismatch) []core.MigrationMismatch {
	out := make([]core.MigrationMismatch, len(in))
	for i, m := range in {
		out[i] = core.MigrationMismatch{
			Name:            m.Name,
			StoredChecksum:  m.StoredChecksum,
			CurrentChecksum: m.CurrentChecksum,
		}
	}
	return out
}
