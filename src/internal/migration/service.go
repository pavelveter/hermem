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
func New(db *sql.DB) *Service {
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
	return core.NormalizeSlice(status), nil
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
// Empty result → no integrity drift. Returns
// `[]store.MigMismatch{}` (not nil) on clean empty so the envelope
// stays `[]`.
func (s *Service) Verify(_ context.Context) ([]store.MigMismatch, error) {
	mismatches, err := store.VerifyMigrationIntegrity(s.db)
	if err != nil {
		return nil, fmt.Errorf("migration verify: %w", err)
	}
	return core.NormalizeSlice(mismatches), nil
}

// Run applies every pending migration in lexicographic order and
// returns the post-apply status snapshot.
//
// §4 audit closure: this is the daemon-LESS apply path that lets
// K8s InitContainers, pre-deploy scripts, and operator invocations
// advance schema outside the boot sequence. Replaces the pre-§4
// apply-on-boot semantic — production must NOT apply migrations as
// part of the boot gate because a long-running ALTER can hold the
// start gate long enough for K8s liveness/readiness probes to kill
// the pod (CrashLoopBackOff). Refusal-mode (auto_migrate=false) is
// the new production default; this method is the operator's escape
// hatch.
//
// On error, returns nil status + wrapped error mirroring the
// underlying store.RunMigrations surface; the message wraps the
// positional context (\"migration apply: <store-err>\") so callers
// branch on the wrapped error message or on a sql.ErrNoRows check
// inside the wrapped chain. The wrapped error is NOT exported as a
// package-level sentinel — store.RunMigrations surfaces per-statement
// SQL errors verbatim today, so a future sentinelisation pass would
// belong in store/, not here.
//
// On success, every embedded migration file reports applied=true in
// the returned slice; the caller can diff against a pre-apply
// DryRun snapshot to print the headline \"applied N\" delta.
//
// Followup note: store.RunMigrations(s.db) does NOT accept a
// context.Context today, so a SIGINT mid-Apply cannot abort a
// long-running ALTER. Pre-§4 limitation; tracked as a separate
// ticket — the §4 closure's intent is that migrations stop holding
// the *boot* gate, but the apply path itself now owns the same
// operator-cancellation window that previously held production
// boots hostage.
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
	var pending []store.MigStatus
	for _, m := range status {
		if !m.Applied {
			pending = append(pending, m)
		}
	}
	return core.NormalizeSlice(pending), nil
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
