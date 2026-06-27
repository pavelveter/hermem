package migration

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

func TestMigrationChecksumSHA256_Deterministic(t *testing.T) {
	h1, err := store.MigrationChecksumSHA256("001_initial_schema.sql")
	if err != nil {
		t.Fatalf("MigrationChecksumSHA256(001): %v", err)
	}
	if h1 == "" {
		t.Fatal("want non-empty hash")
	}
	h2, err := store.MigrationChecksumSHA256("001_initial_schema.sql")
	if err != nil {
		t.Fatalf("MigrationChecksumSHA256(001) repeat: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("non-deterministic hash: %q vs %q", h1, h2)
	}
}

func TestMigrationChecksumSHA256_DifferentFiles(t *testing.T) {
	h1, _ := store.MigrationChecksumSHA256("001_initial_schema.sql")
	h2, _ := store.MigrationChecksumSHA256("002_entity_metadata.sql")
	if h1 == h2 {
		t.Fatal("different migration files must produce different hashes")
	}
}

func TestIntegrity_VerifyCleanAfterMemDB(t *testing.T) {
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
	if len(mismatches) != 0 {
		t.Fatalf("want 0 mismatches on clean DB, got %d: %+v", len(mismatches), mismatches)
	}
}

func TestIntegrity_TamperedChecksumDetected(t *testing.T) {
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	// Corrupt the SHA-256 checksum of the first migration.
	if _, err := db.Exec(`UPDATE migration_checksums SET checksum_sha256 = 'tampered' WHERE version = '001_initial_schema.sql'`); err != nil {
		t.Fatalf("update checksum: %v", err)
	}
	svc := New(db)
	mismatches, err := svc.Verify(t.Context())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(mismatches) == 0 {
		t.Fatal("want at least 1 mismatch after tampering, got 0")
	}
	found := false
	for _, mm := range mismatches {
		if mm.Name == "001_initial_schema.sql" {
			found = true
			if mm.StoredChecksum != "tampered" {
				t.Errorf("stored checksum: want tampered, got %q", mm.StoredChecksum)
			}
			break
		}
	}
	if !found {
		t.Fatal("001_initial_schema.sql not in mismatch list")
	}
}

func TestIntegrity_ChecksumBackfill(t *testing.T) {
	// After task 1, new migrations get checksum_sha256 stored. Verify
	// that an applied migration without a SHA-256 row (simulating a
	// pre-upgrade DB) is silently skipped by verify.
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	defer db.Close()
	// Delete SHA-256 for one migration to simulate pre-upgrade state.
	if _, err := db.Exec(`UPDATE migration_checksums SET checksum_sha256 = NULL WHERE version = '001_initial_schema.sql'`); err != nil {
		t.Fatalf("null checksum: %v", err)
	}
	svc := New(db)
	mismatches, err := svc.Verify(t.Context())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	for _, mm := range mismatches {
		if mm.Name == "001_initial_schema.sql" {
			t.Fatal("migration without SHA-256 should be skipped, not reported as mismatch")
		}
	}
}
