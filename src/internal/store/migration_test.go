package store

import (
	"database/sql"
	"strings"
	"sync"
	"testing"
)

// --- dry-run ---

func TestRunDry_ReturnsPendingMigrations(t *testing.T) {
	db := openTestDB(t)

	// RunDry should list all migrations since we start from a fresh DB
	// that has applied them all via MemDBRandom → InitDB → RunMigrations.
	// After full apply, RunDry should return empty (no pending).
	results, err := RunDry(db)
	if err != nil {
		t.Fatalf("RunDry: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 pending migrations after full apply, got %d", len(results))
	}
}

func TestRunDry_DoesNotMutateDB(t *testing.T) {
	db := openTestDB(t)

	// Snapshot schema_migrations count before dry run.
	var before int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&before); err != nil {
		t.Fatalf("count before: %v", err)
	}

	_, err := RunDry(db)
	if err != nil {
		t.Fatalf("RunDry: %v", err)
	}

	var after int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&after); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if before != after {
		t.Fatalf("RunDry mutated schema_migrations: before=%d after=%d", before, after)
	}
}

func TestRunDry_OnFreshDBListsAll(t *testing.T) {
	// Create a bare DB without running migrations (no InitDB).
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Create the migration tracking tables manually.
	if _, err := db.Exec("CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME)"); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE migration_checksums (version TEXT PRIMARY KEY, checksum TEXT, checksum_sha256 TEXT)"); err != nil {
		t.Fatalf("create migration_checksums: %v", err)
	}

	results, err := RunDry(db)
	if err != nil {
		t.Fatalf("RunDry: %v", err)
	}

	allFiles, err := migrationFiles()
	if err != nil {
		t.Fatalf("migrationFiles: %v", err)
	}
	if len(results) != len(allFiles) {
		t.Fatalf("expected %d dry-run results, got %d", len(allFiles), len(results))
	}
	// Each result should have at least 1 statement.
	for _, r := range results {
		if r.StmtCount == 0 {
			t.Errorf("migration %s: 0 stmts in dry run", r.Name)
		}
		if len(r.Stmts) != r.StmtCount {
			t.Errorf("migration %s: StmtCount=%d but len(Stmts)=%d", r.Name, r.StmtCount, len(r.Stmts))
		}
	}
}

// --- out-of-order detection ---

func TestDetectOutOfOrder_NoIssue(t *testing.T) {
	db := openTestDB(t)
	// All migrations applied in order → no out-of-order.
	ooo, err := DetectOutOfOrder(db)
	if err != nil {
		t.Fatalf("DetectOutOfOrder: %v", err)
	}
	if len(ooo) != 0 {
		t.Fatalf("expected no out-of-order, got %v", ooo)
	}
}

func TestDetectOutOfOrder_DetectsInsert(t *testing.T) {
	db := openTestDB(t)

	// Delete the record for 003 so it becomes "pending" with a lower
	// number than 008 (the highest applied).
	if _, err := db.Exec("DELETE FROM schema_migrations WHERE version = '003_provenance.sql'"); err != nil {
		t.Fatalf("delete 003: %v", err)
	}
	if _, err := db.Exec("DELETE FROM migration_checksums WHERE version = '003_provenance.sql'"); err != nil {
		t.Fatalf("delete 003 checksum: %v", err)
	}

	ooo, err := DetectOutOfOrder(db)
	if err != nil {
		t.Fatalf("DetectOutOfOrder: %v", err)
	}
	if len(ooo) == 0 {
		t.Fatal("expected out-of-order detection, got none")
	}
	found := false
	for _, name := range ooo {
		if strings.Contains(name, "003") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 003 in out-of-order list, got %v", ooo)
	}
}

// --- content drift detection ---

func TestDetectContentDrift_NoDrift(t *testing.T) {
	db := openTestDB(t)
	drifted, err := DetectContentDrift(db)
	if err != nil {
		t.Fatalf("DetectContentDrift: %v", err)
	}
	if len(drifted) != 0 {
		t.Fatalf("expected no drift, got %v", drifted)
	}
}

func TestDetectContentDrift_DetectsTamper(t *testing.T) {
	db := openTestDB(t)

	// Tamper: overwrite the stored checksum for 001 with a fake value.
	if _, err := db.Exec("UPDATE migration_checksums SET checksum_sha256 = '0000000000000000000000000000000000000000000000000000000000000000' WHERE version = '001_initial_schema.sql'"); err != nil {
		t.Fatalf("tamper checksum: %v", err)
	}

	drifted, err := DetectContentDrift(db)
	if err != nil {
		t.Fatalf("DetectContentDrift: %v", err)
	}
	if len(drifted) == 0 {
		t.Fatal("expected drift detection, got none")
	}
	if drifted[0].Name != "001_initial_schema.sql" {
		t.Fatalf("expected drift on 001, got %v", drifted[0].Name)
	}
}

// --- concurrent-apply guard (BEGIN IMMEDIATE) ---

func TestConcurrentApplyGuard(t *testing.T) {
	db := openTestDB(t)

	// Verify busy_timeout is set by InitDB's pragma setup, which is the
	// foundation of our concurrent-apply guard (BEGIN IMMEDIATE + busy_timeout).
	// Full concurrency testing requires two OS processes sharing a file-backed
	// DB; here we verify the configuration is correct.

	var timeout int
	err := db.QueryRow("PRAGMA busy_timeout").Scan(&timeout)
	if err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	// InitDB sets busy_timeout=5000 via pragmas.
	if timeout != 5000 {
		t.Fatalf("expected busy_timeout=5000, got %d", timeout)
	}
}

// --- schema snapshot ---

func TestCaptureSchemaHash_Deterministic(t *testing.T) {
	db := openTestDB(t)
	h1, err := CaptureSchemaHash(db)
	if err != nil {
		t.Fatalf("CaptureSchemaHash: %v", err)
	}
	h2, err := CaptureSchemaHash(db)
	if err != nil {
		t.Fatalf("CaptureSchemaHash: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("non-deterministic schema hash: %s != %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char hex hash, got %d chars", len(h1))
	}
}

func TestCaptureSchemaHash_DetectsChange(t *testing.T) {
	db := openTestDB(t)
	h1, err := CaptureSchemaHash(db)
	if err != nil {
		t.Fatalf("CaptureSchemaHash: %v", err)
	}

	// Alter the schema by adding a table.
	if _, err := db.Exec("CREATE TABLE test_drift (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	h2, err := CaptureSchemaHash(db)
	if err != nil {
		t.Fatalf("CaptureSchemaHash after alter: %v", err)
	}
	if h1 == h2 {
		t.Fatal("schema hash did not change after adding a table")
	}
}

// --- down-migration test harness ---

func TestApplyAllThenRevertAll(t *testing.T) {
	// Capture schema state after all migrations are applied.
	db := openTestDB(t)
	schemaAfterAll, err := CaptureSchemaHash(db)
	if err != nil {
		t.Fatalf("CaptureSchemaHash after all: %v", err)
	}

	// Rollback all migrations (in reverse order).
	applied, err := appliedMigrations(db)
	if err != nil {
		t.Fatalf("appliedMigrations: %v", err)
	}
	// Sort applied names in reverse to rollback newest first.
	var names []string
	for name := range applied {
		names = append(names, name)
	}
	// Use the same sort order as migrationFiles (lexicographic).
	sortStrings(names)

	for i := len(names) - 1; i >= 0; i-- {
		name := names[i]
		if _, err := db.Exec("DELETE FROM schema_migrations WHERE version = ?", name); err != nil {
			t.Fatalf("rollback %s: %v", name, err)
		}
		if _, err := db.Exec("DELETE FROM migration_checksums WHERE version = ?", name); err != nil {
			t.Fatalf("rollback checksum %s: %v", name, err)
		}
	}

	// Migration 013 mutates the P2 episodic schema by DROP COLUMN-ing
	// `started_at` / `timestamp` / `linked_at` (no `IF EXISTS` and no
	// down-migration step). The row-only rollback above only deletes
	// the schema_migrations tracking rows; the physical schema stays
	// partially mutated. Drop the affected tables so the re-apply
	// step runs against a clean slate, then RunMigrations re-creates
	// them and 013 re-runs end-to-end.
	for _, table := range []string{"episodes", "events", "episode_memories", "episode_tasks"} {
		if _, err := db.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			t.Logf("drop %s (reset before re-apply): %v", table, err)
		}
	}

	// Verify no migrations are marked as applied.
	afterRevert, err := appliedMigrations(db)
	if err != nil {
		t.Fatalf("appliedMigrations after revert: %v", err)
	}
	if len(afterRevert) != 0 {
		t.Fatalf("expected 0 applied migrations after full revert, got %d", len(afterRevert))
	}

	// Re-apply all migrations.
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations after revert: %v", err)
	}

	// Schema should match the original.
	schemaAfterReapply, err := CaptureSchemaHash(db)
	if err != nil {
		t.Fatalf("CaptureSchemaHash after reapply: %v", err)
	}
	if schemaAfterAll != schemaAfterReapply {
		t.Fatalf("schema mismatch after revert+reapply:\n  after all:   %s\n  after reapply: %s", schemaAfterAll, schemaAfterReapply)
	}
}

// --- race-safe concurrent migration test ---

func TestConcurrentRunMigrations(t *testing.T) {
	dir := t.TempDir()
	dsn := dir + "/concurrent.db"
	db, err := InitDB(dsn, 3)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Verify all migrations are applied.
	status, err := MigrationStatus(db)
	if err != nil {
		t.Fatalf("MigrationStatus: %v", err)
	}
	for _, s := range status {
		if !s.Applied {
			t.Errorf("migration %s not applied after InitDB", s.Name)
		}
	}

	// Run RunMigrations concurrently — should be idempotent.
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = RunMigrations(db)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent RunMigrations[%d]: %v", i, err)
		}
	}

	// Status should still show all applied.
	status2, err := MigrationStatus(db)
	if err != nil {
		t.Fatalf("MigrationStatus after concurrent: %v", err)
	}
	for _, s := range status2 {
		if !s.Applied {
			t.Errorf("migration %s not applied after concurrent run", s.Name)
		}
	}
}

// --- migration number parsing ---

func TestMigrationNum(t *testing.T) {
	cases := []struct {
		name string
		want int
	}{
		{"001_initial_schema.sql", 1},
		{"008_add_beliefs_table.sql", 8},
		{"099_test.sql", 99},
		{"no_number.sql", 0},
		{"", 0},
	}
	for _, tc := range cases {
		got := migrationNum(tc.name)
		if got != tc.want {
			t.Errorf("migrationNum(%q) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// --- splitSQL ---

func TestSplitSQL_Basic(t *testing.T) {
	input := "CREATE TABLE t (id INT);\nCREATE INDEX idx ON t(id);\n"
	stmts := splitSQL(input)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 stmts, got %d: %v", len(stmts), stmts)
	}
}

func TestSplitSQL_TriggerAware(t *testing.T) {
	// BEGIN must be on its own line for splitSQL to detect it as a
	// trigger-body start (per the trigger-aware parser contract).
	input := `CREATE TRIGGER trg AFTER INSERT ON t
BEGIN
  UPDATE t SET x = 1 WHERE id = NEW.id;
END;
SELECT 1;
`
	stmts := splitSQL(input)
	if len(stmts) != 2 {
		t.Fatalf("expected 2 stmts (trigger + select), got %d: %v", len(stmts), stmts)
	}
}

func TestSplitSQL_SkipsComments(t *testing.T) {
	input := "-- comment\nCREATE TABLE t (id INT);\n-- another comment\n"
	stmts := splitSQL(input)
	if len(stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d: %v", len(stmts), stmts)
	}
}

// sortStrings is a test helper for deterministic ordering.
func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}
