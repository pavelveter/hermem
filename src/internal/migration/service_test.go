package migration

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/store"
)

// PHASE 3.2 tests use store.MemDB() (the same pattern PHASE 2.x +
// 3.1 fixtures use). store.MemDB() runs embedded migrations as part
// of in-memory setup, so the schema_migrations / migration_checksums
// tables are not empty on entry. Each test below asserts the
// nil→empty normalisation + non-error contract but does NOT pin
// specific counts because the actual migration set varies across
// the embedded SQL files.

func TestNewService_Success(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestService_Status_ReturnsNonNilSlice(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	status, err := svc.Status(t.Context())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	// Pin nil→empty normalization: the slice must not be nil even if
	// the underlying call returns an empty list. Whether the count is
	// 0 or N (MemDB runs migrations) does not matter for this test —
	// the contract is "non-nil always".
	if status == nil {
		t.Fatal("want non-nil slice, got nil")
	}
	// After MemDB runs embedded migrations, every migration file is
	// applied → len(status) > 0. Pin that to protect against
	// regressions in store.MigrationStatus's filtering.
	if len(status) == 0 {
		t.Error("want at least one migration row after MemDB setup")
	}
}

func TestService_Rollback_NoError(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	// Rollback semantics pre-PHASE-3.2: returns (name, nil) where
	// name is the rolled-back migration file, or ("", nil) when no
	// migrations exist. After MemDB runs embedded migrations we
	// should observe a non-empty name (the most-recent applied
	// migration). Either outcome is acceptable for this test — the
	// contract is "no error, returns the name or empty".
	name, err := svc.Rollback(t.Context(), "")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	// Either non-empty name (rolled back something) or empty (no
	// migrations applied). We don't assert; just document that the
	// return value is non-error in both cases.
	t.Logf("Rollback returned name=%q (either is valid)", name)
}

func TestService_Verify_NoMismatchOnConsistentDB(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	mismatches, err := svc.Verify(t.Context())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if mismatches == nil {
		t.Fatal("want non-nil slice, got nil")
	}
	// MemDB just ran migrations via the embedded FS so the stored
	// checksums are freshly computed and the migration files haven't
	// changed since. Pin 0 mismatches — a non-zero count would mean
	// the checksum comparison logic diverged.
	if len(mismatches) != 0 {
		t.Errorf("want 0 mismatches on just-migrated DB, got %d: %+v",
			len(mismatches), mismatches)
	}
}

func TestService_DryRun_ReturnsEmptyAfterMemDB(t *testing.T) {
	// MemDB runs all embedded migrations at init, so DryRun should
	// return an empty (non-nil) pending list.
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	pending, err := svc.DryRun(t.Context())
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if pending == nil {
		t.Fatal("want non-nil slice, got nil")
	}
	if len(pending) != 0 {
		t.Errorf("want 0 pending after MemDB init, got %d", len(pending))
	}
}

func TestService_Schema_NoError(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)
	report, err := svc.Schema(t.Context(), core.DefaultSchemaConfig(false))
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	// MemDB's store.CheckSchemaFingerprint under the hood inserts
	// the current fingerprint if `meta` was empty (first-boot codepath).
	// After the insert, a subsequent call would see stored=current.
	// For THIS test we just pin:
	//   - non-empty current hash
	//   - no error
	// We do NOT pin stored="" because MemDB's setup may have already
	// touched meta (it runs migrations which may insert fingerprint).
	if report.Current == "" {
		t.Error("want non-empty current hash")
	}
	// DriftDetected correctness: if stored == "" (first ever call
	// where no inserted row yet), DriftDetected must be false. If
	// stored == current (after insert), DriftDetected is also false.
	// Both outcomes are valid; the only invalid outcome is
	// DriftDetected=true with stored==current, which would mean the
	// comparison logic is broken.
	if report.DriftDetected && report.Stored == report.Current {
		t.Errorf("drift_detected=true with stored==current is invalid: stored=%q current=%q",
			report.Stored, report.Current)
	}
}

// TestService_Run_FreshDBIsNoOp mirrors the existing TestService_DryRun_*
// pattern but exercises the APPLY path instead of the READ-ONLY path.
//
// MemDB() runs all embedded migrations at init, so a "fresh DB" for our
// purposes means "all migrations applied". `hermem db migrate apply`
// invokes the same Service.Run code path; on a freshly-bootstrap"
// deployment where the embedded set is already fully recorded in
// schema_migrations, the operator expects Run to be a clean no-op
// (zero SQL fires, zero rows mutate) rather than a silent duplicate
// apply or a regression-flavored error.
//
// Contract pinned:
//   - Pre-Run DryRun returns 0 pending (MemDB applied everything).
//   - Run returns nil error and a non-nil post-Status slice.
//   - Every row in post-Status still reports Applied=true. A flip to
//     Applied=false would mean store.RunMigrations silently regressed
//     and removed rows — surface that immediately.
//
// Part of the §4 audit followup: the operator escape-hatch semantics
// rely on this path; a regression here would let a K8s InitContainer
// `hermem db migrate apply` invocation silently desync schema state.
func TestService_Run_FreshDBIsNoOp(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)

	pre, err := svc.DryRun(t.Context())
	if err != nil {
		t.Fatalf("pre DryRun: %v", err)
	}
	if len(pre) != 0 {
		t.Fatalf("pre-Run DryRun: want 0 pending (MemDB applied all embedded migrations), got %d", len(pre))
	}

	post, err := svc.Run(t.Context())
	if err != nil {
		t.Fatalf("Run on fresh DB: %v", err)
	}
	if post == nil {
		t.Fatal("Run: want non-nil slice, got nil")
	}
	// post-Run Status must report every migration file as applied
	// (embedded migrations don't change on a no-op apply). A row that
	// flipped to applied=false would mean store.RunMigrations
	// regressed and started DELETEing rows from schema_migrations
	// during the "skip-already-applied" branch — surface immediately.
	var appliedCount int
	for _, m := range post {
		if m.Applied {
			appliedCount++
		}
	}
	if appliedCount != len(post) {
		t.Errorf("post-Run applied count: want %d (=all rows still applied), got %d (%d flipped)",
			len(post), appliedCount, len(post)-appliedCount)
	}
}

// TestService_Run_PendingMigrationsApplied pins the production apply
// path: set up a pending state via Rollback on a fully-migrated DB,
// then call Run and assert the previously-rolled-back row is back to
// applied=true and the post-Run pending count is zero.
//
// Setup is identical to TestService_Rollback_NoError's contract
// (non-error; rolls back the most-recent applied migration). The
// additional contract here is the re-apply side: Service.Run is the
// ONLY path that closes the §4 audit's apply-escape-hatch semantics,
// so a regression where Run returns success but the
// schema_migrations row doesn't get re-inserted would silently leave
// the schema half-applied — exactly the bug class §4 was supposed
// to forbid.
//
// Contract pinned:
//   - svc.Rollback on a fresh MemDB succeeds and returns the name of
//     the rolled-back file.
//   - DryRun on the post-Rollback DB returns 1 pending with the
//     rolled-back file's name.
//   - svc.Run returns nil error.
//   - post-Run Status shows the rolled-back file is now Applied=true
//     AND zero rows still report Applied=false.
func TestService_Run_PendingMigrationsApplied(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	svc := New(db)

	// Set up the pending state. svc.Rollback(``) DELETEs the
	// schema_migrations + migration_checksums rows for the
	// most-recent applied file; the SQL itself is NOT reversed
	// (per the §3 hardening comment in store.RollbackMigration), so
	// re-applying is a pure idempotent replay that re-INSERTs the
	// schema_migrations row.
	rolledBackName, err := svc.Rollback(t.Context(), "")
	if err != nil {
		t.Fatalf("setup rollback: %v", err)
	}
	if rolledBackName == "" {
		t.Fatal("rollback returned empty name on a just-migrated DB; cannot validate re-apply")
	}

	// Migration 013 mutates the P2 episodic schema by DROP COLUMN-ing
	// `started_at` / `timestamp` / `linked_at` (no `IF EXISTS` and no
	// down-migration step). svc.Rollback only deletes the
	// schema_migrations tracking row; the physical schema stays partially
	// mutated. Drop the affected tables so the re-apply step runs against
	// a clean slate, mirroring the §3 hardening comment in
	// store.RollbackMigration and the existing idiom in
	// store.migration_test.TestApplyAllThenRevertAll (which performs the
	// same teardown for the same reason).
	for _, table := range []string{"episodes", "events", "episode_memories", "episode_tasks"} {
		if _, err := db.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			t.Fatalf("drop %s (reset before re-apply): %v", table, err)
		}
	}

	pending, err := svc.DryRun(t.Context())
	if err != nil {
		t.Fatalf("post-Rollback DryRun: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("post-Rollback DryRun: want 1 pending, got %d", len(pending))
	}
	if pending[0].Name != rolledBackName {
		t.Errorf("DryRun pending: want %q (=rolled back), got %q", rolledBackName, pending[0].Name)
	}

	if _, err := svc.Run(t.Context()); err != nil {
		t.Fatalf("Run on pending-apply DB: %v", err)
	}

	post, err := svc.Status(t.Context())
	if err != nil {
		t.Fatalf("post-Run Status: %v", err)
	}
	if post == nil {
		t.Fatal("post-Run Status: want non-nil slice, got nil")
	}
	var (
		reApplied     bool
		stillPendingN int
	)
	for _, m := range post {
		if m.Name == rolledBackName && m.Applied {
			reApplied = true
		}
		if !m.Applied {
			stillPendingN++
		}
	}
	if !reApplied {
		t.Errorf("post-Run Status: %q should be applied=true after Run, isn't", rolledBackName)
	}
	if stillPendingN != 0 {
		t.Errorf("post-Run Status: want 0 pending after Run, got %d", stillPendingN)
	}
}
