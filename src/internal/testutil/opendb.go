package testutil

import (
	"database/sql"
	"testing"

	"github.com/pavelveter/hermem/src/internal/store"
)

// OpenTestDB returns a concurrent-safe in-memory SQLite with the full
// hermem schema. Uses MemDBRandom so tests under -race don't share the
// global :memory: cache. Cleanup is registered automatically.
func OpenTestDB(t testing.TB) *sql.DB {
	t.Helper()
	db, err := store.MemDBRandom()
	if err != nil {
		t.Fatalf("memdb random: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// OpenTestDBSimple returns a simpler in-memory SQLite (MemDB). Suitable
// for tests that don't run concurrently or under -race.
func OpenTestDBSimple(t testing.TB) *sql.DB {
	t.Helper()
	db, err := store.MemDB()
	if err != nil {
		t.Fatalf("memdb: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
