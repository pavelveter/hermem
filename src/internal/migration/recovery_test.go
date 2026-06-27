package migration

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

func TestRecovery_RollbackEmptyDB(t *testing.T) {
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	// Delete all migration records to simulate a fresh DB.
	if _, err := db.Exec("DELETE FROM schema_migrations"); err != nil {
		t.Fatalf("delete schema_migrations: %v", err)
	}
	if _, err := db.Exec("DELETE FROM migration_checksums"); err != nil {
		t.Fatalf("delete migration_checksums: %v", err)
	}
	svc := New(db)
	name, err := svc.Rollback(t.Context(), "")
	if err != nil {
		t.Fatalf("Rollback on empty DB: %v", err)
	}
	if name != "" {
		t.Fatalf("want empty name on empty DB, got %q", name)
	}
}

func TestRecovery_RollbackPartiallyApplied(t *testing.T) {
	// Simulate a crash mid-apply: a migration recorded in
	// schema_migrations but missing from migration_checksums.
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	// Get the last applied migration.
	var lastVer string
	err = db.QueryRow("SELECT version FROM schema_migrations ORDER BY applied_at DESC LIMIT 1").Scan(&lastVer)
	if err != nil {
		t.Fatalf("read last migration: %v", err)
	}
	// Remove its checksum to simulate partial apply.
	if _, err := db.Exec("DELETE FROM migration_checksums WHERE version = ?", lastVer); err != nil {
		t.Fatalf("delete checksum: %v", err)
	}
	// DryRun should NOT show it as pending (it's in schema_migrations).
	svc := New(db)
	pending, err := svc.DryRun(t.Context())
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	for _, m := range pending {
		if m.Name == lastVer {
			t.Fatal("partially applied migration should not appear in DryRun (it's in schema_migrations)")
		}
	}
	// Rollback should still clean it up.
	name, err := svc.Rollback(t.Context(), "")
	if err != nil {
		t.Fatalf("Rollback after partial apply: %v", err)
	}
	if name == "" {
		t.Fatal("want rolled-back migration name, got empty")
	}
	// After rollback, DryRun should show it as pending.
	pending, err = svc.DryRun(t.Context())
	if err != nil {
		t.Fatalf("DryRun after rollback: %v", err)
	}
	found := false
	for _, m := range pending {
		if m.Name == lastVer {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("migration %q should be pending after rollback", lastVer)
	}
}

func TestRecovery_RollbackToTarget(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	// Get the second-to-last migration as target.
	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY applied_at DESC")
	if err != nil {
		t.Fatalf("query migrations: %v", err)
	}
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		versions = append(versions, v)
	}
	rows.Close()
	if len(versions) < 2 {
		t.Skip("need at least 2 applied migrations for target test")
	}
	target := versions[len(versions)-2] // second from last = first applied
	svc := New(db)
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i] == target {
			break
		}
		name, err := svc.Rollback(t.Context(), target)
		if err != nil {
			t.Fatalf("Rollback to %q: %v", target, err)
		}
		if name != target {
			t.Fatalf("want target %q, got %q", target, name)
		}
	}
	// Verify only migrations up to target remain.
	var remaining int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	expected := len(versions) - (len(versions) - 1) // target is first applied
	// Find target index.
	targetIdx := -1
	for i, v := range versions {
		if v == target {
			targetIdx = i
			break
		}
	}
	if targetIdx >= 0 {
		expected = targetIdx + 1
	}
	if remaining != expected {
		t.Errorf("want %d remaining migrations, got %d", expected, remaining)
	}
}
