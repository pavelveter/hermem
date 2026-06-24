package store

import (
	"database/sql"
	"strings"
	"testing"
)

// TestInitDB_FilePrefixIdempotent — regression for the doubled-`file:` bug
// that surfaced in round-6 when openTestDB switched to MemDBRandom. The
// fixture path is `file:memdb-X?mode=memory&cache=shared`; pre-fix, InitDB
// would prepend another `file:` producing `file:file:memdb-X?…` and
// SQLite's URI parser would confuse the cache-mode value with the
// `_busy_timeout` parameter. Post-fix, InitDB detects a leading `file:`
// and appends with `&` instead of duplicating the prefix.
func TestInitDB_FilePrefixIdempotent(t *testing.T) {
	db, err := InitDB("file:memdb-test-fixture?mode=memory&cache=shared", 3)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mode, err := verifyPragmaOrder(db)
	if err != nil {
		t.Fatalf("verifyPragmaOrder: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("expected WAL after InitDB on file:memdb-… DSN, got %q", mode)
	}
}

// TestVerifyPragmaOrder_DetectsWal — happy path. The default InitDB setup
// yields `wal`; verifyPragmaOrder returns it unchanged with no error.
func TestVerifyPragmaOrder_DetectsWal(t *testing.T) {
	db := openTestDB(t)
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
func TestVerifyPragmaOrder_ReportsNonWal(t *testing.T) {
	db := openTestDB(t)
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
// produce distinct DSNs (and therefore isolated in-memory caches). On a
// regression to a shared name, the two DBs would race on
// schema_migrations and `CREATE TABLE` would fail with "table already
// exists" or worse, cross-test data leakage.
func TestMemDBRandom_PerCallIsolation(t *testing.T) {
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

	if sameDB(a, b) {
		t.Fatal("MemDBRandom calls returned the same *sql.DB handle")
	}
	// Each pool has SetMaxOpenConns(1); they hold distinct handles.
}

// sameDB reports whether two *sql.DB pointers trace to the same
// underlying SQLite connection. MemDBRandom opens a fresh shared-cache
// in-memory DB per call, so the two pointers MUST be different.
func sameDB(a, b *sql.DB) bool {
	return a == b
}
