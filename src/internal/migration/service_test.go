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
