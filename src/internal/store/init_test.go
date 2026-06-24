package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitDB_FilePrefixIdempotent — regression for the doubled-`file:`bug
// that surfaced in round-6 when openTestDB switched to MemDBRandom.
// Pre-fix, InitDB unconditionally prepended `file:` to non-`:memory:`
// paths, so a fixture like `file:<path>` became `file:file:<path>…`
// and SQLite's URI parser choked on the doubled prefix. Post-fix,
// InitDB detects a leading `file:` and appends its PRAGMAs with `&`.
//
// Exercises InitDB's `case strings.HasPrefix(dbPath, "file:")` branch.
// Uses a bare DSN because the contract being tested is "prefix detected,
// NOT doubled" — adding caller-side pragma flags would just create param
// collisions with InitDB's own pragmas (last-write-wins silently per
// MemDBRandom's WARNING). Uses a file-backed DB because SQLite silently
// reverts PRAGMA journal_mode=WAL on memory databases.
func TestInitDB_FilePrefixIdempotent(t *testing.T) {
	t.Parallel()
	dsn := "file:" + filepath.Join(t.TempDir(), "init.db")
	db, err := InitDB(dsn, 3)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mode, err := verifyPragmaOrder(db)
	if err != nil {
		t.Fatalf("verifyPragmaOrder: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("expected WAL after InitDB on %q, got %q", dsn, mode)
	}
}

// TestVerifyPragmaOrder_DetectsWal — happy path. The default InitDB setup
// yields `wal`; verifyPragmaOrder returns it unchanged with no error.
//
// Exercises InitDB's `default` branch (bare path → Prefixed with `file:`
// + `?`). Companion to TestInitDB_FilePrefixIdempotent, which hits the
// prefix-aware branch; together they cover both URI-builder paths.
// Uses a file-backed DB (not MemDBRandom's shared in-memory fixture)
// because SQLite reverts PRAGMA journal_mode=WAL on memory DBs.
func TestVerifyPragmaOrder_DetectsWal(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "wal.db")
	db, err := InitDB(dsn, 3)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mode, err := verifyPragmaOrder(db)
	if err != nil {
		t.Fatalf("verifyPragmaOrder: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("mode: want %q, got %q", "wal", mode)
	}
}

// TestVerifyPragmaOrder_ReportsNonWal — forced DELETE mode returns
// "delete" with no error and (in production) trips the WARN slog. The
// contract is "observer reports, does not enforce" — so no error, but
// the returned mode MUST reflect observed state so external probes can
// branch.
//
// MUST run against a file-backed DB: shared in-memory (`cache=shared`)
// rejects `journal_mode=DELETE` and silently keeps `memory`, which
// hides the regression we want to catch. t.TempDir() + per-test DSN
// also keeps this parallel-safe.
func TestVerifyPragmaOrder_ReportsNonWal(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "sqlite.db")
	db, err := InitDB(dsn, 3)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("PRAGMA journal_mode = DELETE"); err != nil {
		t.Fatalf("force DELETE mode: %v", err)
	}
	mode, err := verifyPragmaOrder(db)
	if err != nil {
		t.Fatalf("verifyPragmaOrder: %v", err)
	}
	if !strings.EqualFold(mode, "delete") {
		t.Fatalf("mode: want %q, got %q", "delete", mode)
	}
}

// TestMemDBRandom_PerCallIsolation — two consecutive MemDBRandom calls
// produce distinct shared-cache namespaces, so inserts in one are
// invisible to the other. Both directions (A→B and B→A) are exercised
// so a regression to a single shared name fails either way.
//
// Safe under `t.Parallel()` because MemDBRandom uses random hex suffix
// per call.
func TestMemDBRandom_PerCallIsolation(t *testing.T) {
	t.Parallel()

	const ddlSQL = "CREATE TABLE iso (id INTEGER PRIMARY KEY, val TEXT NOT NULL)"
	const insertSQL = "INSERT INTO iso (id, val) VALUES (?, ?)"
	const selectSQL = "SELECT val FROM iso WHERE id = ?"

	t.Run("A_to_B", func(t *testing.T) {
		t.Parallel()
		a, err := MemDBRandom()
		if err != nil {
			t.Fatalf("MemDBRandom A: %v", err)
		}
		t.Cleanup(func() { _ = a.Close() })

		b, err := MemDBRandom()
		if err != nil {
			t.Fatalf("MemDBRandom B: %v", err)
		}
		t.Cleanup(func() { _ = b.Close() })

		mustExec(t, a, ddlSQL)
		mustExec(t, b, ddlSQL)

		mustExec(t, a, insertSQL, 1, "from-A")

		var aCount int
		if err := a.QueryRow("SELECT COUNT(*) FROM iso").Scan(&aCount); err != nil {
			t.Fatalf("A COUNT: %v", err)
		}
		if aCount != 1 {
			t.Fatalf("A: want 1 row, got %d", aCount)
		}

		var bVal string
		switch err := b.QueryRow(selectSQL, 1).Scan(&bVal); {
		case err == nil:
			t.Fatalf("B saw A's row %q — shared-cache namespace leaked", bVal)
		case !errors.Is(err, sql.ErrNoRows):
			t.Fatalf("B query: unexpected err %v", err)
		}
	})

	t.Run("B_to_A", func(t *testing.T) {
		t.Parallel()
		a, err := MemDBRandom()
		if err != nil {
			t.Fatalf("MemDBRandom A: %v", err)
		}
		t.Cleanup(func() { _ = a.Close() })

		b, err := MemDBRandom()
		if err != nil {
			t.Fatalf("MemDBRandom B: %v", err)
		}
		t.Cleanup(func() { _ = b.Close() })

		mustExec(t, a, ddlSQL)
		mustExec(t, b, ddlSQL)

		mustExec(t, b, insertSQL, 7, "from-B")

		var bCount int
		if err := b.QueryRow("SELECT COUNT(*) FROM iso").Scan(&bCount); err != nil {
			t.Fatalf("B COUNT: %v", err)
		}
		if bCount != 1 {
			t.Fatalf("B: want 1 row, got %d", bCount)
		}

		var aVal string
		switch err := a.QueryRow(selectSQL, 7).Scan(&aVal); {
		case err == nil:
			t.Fatalf("A saw B's row %q — shared-cache namespace leaked", aVal)
		case !errors.Is(err, sql.ErrNoRows):
			t.Fatalf("A query: unexpected err %v", err)
		}
	})
}

// mustExec runs an Exec and fails the test on error. Keeps the isolation
// subtests readable without losing test-failure on schema/insert issues.
func mustExec(t *testing.T, db *sql.DB, stmt string, args ...any) {
	t.Helper()
	if _, err := db.Exec(stmt, args...); err != nil {
		t.Fatalf("exec %q: %v", stmt, err)
	}
}
